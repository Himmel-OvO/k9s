package view

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/derailed/tcell/v2"
	"github.com/derailed/tview"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/ui"
)

/*
The program is responsible for extending the functionality of k9s to allow for overriding container images in Kubernetes.

The program defines a struct called ImageExtender that has a ResourceViewer field. The ResourceViewer interface is implemented by other structs and is used by k9s to display resources. The ImageExtender struct extends the functionality of ResourceViewer to allow for modifying container images.

The ImageExtender struct has a bindKeys method that adds a new key binding to the ResourceViewer. When the user presses the i key, the setImageCmd method is called.

The setImageCmd method is responsible for displaying a dialog box that allows the user to modify container images. The dialog box is created by the showImageDialog method.

The makeSetImageForm method creates a form that allows the user to modify container images. The form displays all of the containers in the selected pod and allows the user to modify the container image.

The setImages method is responsible for setting the container images for the selected pod. It creates a new ImageSpecs object that contains the modified container images and passes this object to the ResourceViewer to update the pod.
*/
const imageKey = "setImage"

type imageFormSpec struct {
	name, dockerImage, newDockerImage string
	init, traceLog, newTraceLog       bool
}

func (m *imageFormSpec) modified() bool {
	newDockerImage := strings.TrimSpace(m.newDockerImage)
	return newDockerImage != "" && m.dockerImage != newDockerImage
}

func (m *imageFormSpec) log_pressed() bool {
	return m.traceLog != m.newTraceLog
}

func (m *imageFormSpec) imageSpec() dao.ImageSpec {
	ret := dao.ImageSpec{
		Name: m.name,
		Init: m.init,
		//TraceLog: m.traceLog,
	}

	if m.modified() {
		ret.DockerImage = strings.TrimSpace(m.newDockerImage)
	} else {
		ret.DockerImage = m.dockerImage
	}

	/*if m.log_pressed() {
		ret.TraceLog = m.newTraceLog
	} else {
		ret.TraceLog = m.traceLog
	}*/
	return ret
}

// ImageExtender provides for overriding container images.
type ImageExtender struct {
	ResourceViewer
}

// NewImageExtender returns a new extender.
func NewImageExtender(r ResourceViewer) ResourceViewer {
	s := ImageExtender{ResourceViewer: r}
	s.AddBindKeysFn(s.bindKeys)

	return &s
}

func (s *ImageExtender) bindKeys(aa ui.KeyActions) {
	if s.App().Config.K9s.IsReadOnly() {
		return
	}
	aa.Add(ui.KeyActions{
		ui.KeyI: ui.NewKeyAction("Set Image", s.setImageCmd, true),
		ui.KeyT: ui.NewKeyAction("⛵Trace Logs", s.setTraceLogsCmd, true),
		ui.KeyO: ui.NewKeyAction("⛵Trace Logs", s.setTraceLogsCmd, false),
	})
}

func (s *ImageExtender) setImageCmd(evt *tcell.EventKey) *tcell.EventKey {
	path := s.GetTable().GetSelectedItem()
	if path == "" {
		return nil
	}

	s.Stop()
	defer s.Start()
	if err := s.showImageDialog(path); err != nil {
		s.App().Flash().Err(err)
	}

	return nil
}

func (s *ImageExtender) showImageDialog(path string) error {
	form, err := s.makeSetImageForm(path)
	if err != nil {
		return err
	}
	confirm := tview.NewModalForm("<Set image>", form)
	confirm.SetText(fmt.Sprintf("Set image %s %s", s.GVR(), path))
	confirm.SetDoneFunc(func(int, string) {
		s.dismissDialog()
	})
	s.App().Content.AddPage(imageKey, confirm, false, false)
	s.App().Content.ShowPage(imageKey)

	return nil
}

func (s *ImageExtender) makeSetImageForm(sel string) (*tview.Form, error) {
	f := s.makeStyledForm()
	podSpec, err := s.getPodSpec(sel)
	if err != nil {
		return nil, err
	}
	formContainerLines := make([]*imageFormSpec, 0, len(podSpec.InitContainers)+len(podSpec.Containers))
	for _, spec := range podSpec.InitContainers {
		formContainerLines = append(formContainerLines, &imageFormSpec{init: true, name: spec.Name, dockerImage: spec.Image})
	}
	for _, spec := range podSpec.Containers {
		formContainerLines = append(formContainerLines, &imageFormSpec{name: spec.Name, dockerImage: spec.Image})
	}
	for i := range formContainerLines {
		ctn := formContainerLines[i]
		f.AddInputField(ctn.name, ctn.dockerImage, 0, nil, func(changed string) {
			ctn.newDockerImage = changed
		})
	}

	f.AddButton("OK", func() {
		defer s.dismissDialog()
		var imageSpecsModified dao.ImageSpecs
		for _, v := range formContainerLines {
			if v.modified() {
				imageSpecsModified = append(imageSpecsModified, v.imageSpec())
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.App().Conn().Config().CallTimeout())
		defer cancel()
		if err := s.setImages(ctx, sel, imageSpecsModified); err != nil {
			log.Error().Err(err).Msgf("PodSpec %s image update failed", sel)
			s.App().Flash().Err(err)
			return
		}
		s.App().Flash().Infof("Resource %s:%s image updated successfully", s.GVR(), sel)
	})
	f.AddButton("Cancel", func() {
		s.dismissDialog()
	})

	return f, nil
}

func (s *ImageExtender) dismissDialog() {
	s.App().Content.RemovePage(imageKey)
}

func (s *ImageExtender) makeStyledForm() *tview.Form {
	f := tview.NewForm()
	f.SetItemPadding(0)
	f.SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tview.Styles.PrimitiveBackgroundColor).
		SetButtonTextColor(tview.Styles.PrimaryTextColor).
		SetLabelColor(tcell.ColorAqua).
		SetFieldTextColor(tcell.ColorOrange)
	return f
}

func (s *ImageExtender) getPodSpec(path string) (*corev1.PodSpec, error) {
	res, err := dao.AccessorFor(s.App().factory, s.GVR())
	if err != nil {
		return nil, err
	}
	resourceWPodSpec, ok := res.(dao.ContainsPodSpec)
	if !ok {
		return nil, fmt.Errorf("expecting a ContainsPodSpec for %q but got %T", s.GVR(), res)
	}

	return resourceWPodSpec.GetPodSpec(path)
}

func (s *ImageExtender) setImages(ctx context.Context, path string, imageSpecs dao.ImageSpecs) error {
	res, err := dao.AccessorFor(s.App().factory, s.GVR())
	if err != nil {
		return err
	}

	resourceWPodSpec, ok := res.(dao.ContainsPodSpec)
	if !ok {
		return fmt.Errorf("expecting a scalable resource for %q", s.GVR())
	}

	return resourceWPodSpec.SetImages(ctx, path, imageSpecs)
}

func (s *ImageExtender) setTraceLogsCmd(evt *tcell.EventKey) *tcell.EventKey {
	path := s.GetTable().GetSelectedItem()
	if path == "" {
		return nil
	}

	s.Stop()
	defer s.Start()
	if err := s.showTraceLogsDialog(path); err != nil {
		s.App().Flash().Err(err)
	}

	return nil
}

func (s *ImageExtender) showTraceLogsDialog(path string) error {
	form, err := s.makeSetTraceLogsForm(path)
	if err != nil {
		return err
	}
	confirm := tview.NewModalForm("<Trace Logs>", form)
	confirm.SetDoneFunc(func(int, string) {
		s.dismissDialog()
	})
	/*confirm.SetText(fmt.Sprintf("Trace Logs %s %s", s.GVR(), path))
	})*/
	s.App().Content.AddPage(imageKey, confirm, false, false)
	s.App().Content.ShowPage(imageKey)

	return nil
}

func findLatestFile() (string, error) {
	dir, err := os.Getwd() // 将查找目录设置为当前工作目录
	dir = "/root"
	if err != nil {
		return "", err
	}

	filePattern := "traceUdmService*"

	var latestModTime time.Time
	var latestFile string

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		match, _ := filepath.Match(filePattern, info.Name())
		if !info.IsDir() && match {
			modTime := info.ModTime()
			if modTime.After(latestModTime) {
				latestModTime = modTime
				latestFile = path
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	if latestFile != "" {
		return latestFile, nil
	} else {
		return "", fmt.Errorf("No files matching pattern %s found in directory %s", filePattern, dir)
	}
}

func (s *ImageExtender) FindTraceLogScript() (path string) {
	rootDir := "/root"
	targetName := "traceUdmService.sh.1"
	fullpath := ""

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == targetName {
			//fmt.Println("Found file:", path)
			fullpath = path
		}
		return nil
	})
	if err != nil {
		fmt.Println("Error occurred while walking directory:", err)
	}
	return fullpath
}

// ❌✔️ ✅ 🚫
func (s *ImageExtender) makeSetTraceLogsForm(path string) (*tview.Form, error) {
	f := s.makeStyledForm()
	ns, _ := client.Namespaced(path)
	podLabel := ""
	podname := ""
	f.AddInputField("Pod Name", "", 8, nil, func(changed string) {
		switch changed {
		case "SDM", "sdm", "sd", "SD", "s", "S":
			{
				podname = "udmsdm"
				//podLabel = "NGC_SDM"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_SDM", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_LLB", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_SDL", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("IMS_G_CMPROXY", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "EE", "ee", "EES", "ees", "Ee", "eE", "Ees", "E", "e":
			{
				podname = "udmees"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_EES", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_LLB", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_SDL", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("IMS_G_CMPROXY", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "SIM", "sim", "Sim":
			{
				podname = "udmsim"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_XIM", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_XIP", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_TCPCLIENT", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("IMS_G_CMPROXY", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "UECM", "Uecm", "uecm", "uec", "UEC":
			{
				podname = "udmuecm"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_UECM", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_LLB", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_SDL", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("IMS_G_CMPROXY", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "NIM", "nim", "Nim", "NI", "ni":
			{
				podname = "udmnim"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_NIM", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_LAG", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_DNSCLIENT", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "MTS", "MT", "mt", "mts", "Mt", "Mts", "M", "m":
			{
				podname = "udmmt"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_MTS", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_LLB", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_SDL", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "PP", "pp", "Pp", "pps", "PPS", "P", "p":
			{
				podname = "udmpp"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_PPS", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_LLB", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_SDL", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "UEAUTH", "ueauth":
			{
				podname = "udmueauth"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_UEAUTH", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_LLB", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_SDL", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "UESFAUTH", "uesfauth", "ausfa", "AUSFA":
			{
				podname = "ausfauth"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_AUSF", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_H2P", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		case "ARPF", "arpf":
			{
				podname = "udmarpf"
				//podLabel = "NGC_EES"
				f.RemoveLastFormItem(f.GetFormItemCount())
				f.AddCheckbox("NGC_TFR", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_OLH", false, func(label string, checked bool) {
					if checked {
						podLabel = podLabel + " " + label
					}
				})
				f.AddCheckbox("NGC_CIP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("HSS_ACP", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("HSS_ACU", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("HSS_ASH", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
				f.AddCheckbox("HTTP_SV", false, func(label string, checked bool) {
					if checked {
						podLabel += " " + strings.TrimSpace(label)
					}
				})
			}
		default:
			{
				f.RemoveLastFormItem(f.GetFormItemCount())
			}
		}
	})
	/*
		podSpec, err := s.getPodSpec(sel)
		if err != nil {
			return nil, err
		}
		formContainerLines := make([]*imageFormSpec, 0, len(podSpec.InitContainers)+len(podSpec.Containers))
		for _, spec := range podSpec.InitContainers {
			formContainerLines = append(formContainerLines, &imageFormSpec{init: true, name: spec.Name, dockerImage: spec.Image, traceLog: spec.TTY})
		}
		for _, spec := range podSpec.Containers {
			formContainerLines = append(formContainerLines, &imageFormSpec{name: spec.Name, dockerImage: spec.Image, traceLog: spec.TTY})
		}
		for i := range formContainerLines {
			ctn := formContainerLines[i]
			checkbox := tview.NewCheckbox().SetLabel(ctn.name + "                            ").SetChecked(ctn.traceLog)
			checkbox.SetCheckedString("on")
			checkbox.SetChangedFunc(func(label string, checked bool) {
				ctn.newTraceLog = checked
			})
			f.AddFormItem(checkbox)
		}*/

	f.AddButton("Start", func() {
		defer s.dismissDialog()
		scriptPath, _ := findLatestFile() //s.FindTraceLogScript()
		s.App().Flash().Infof("trace log status updated successfully", scriptPath)
		startcmd := exec.Command("sh", scriptPath, "start", podname, ns, podLabel)
		out, err := startcmd.CombinedOutput()
		if err != nil {
			fmt.Println("Command execution failed with error:", err)
			s.App().Flash().Infof("trace log status open successfully!")
			return
		}
		ioutil.Discard.Write(out)
		/*
			ctx, cancel := context.WithTimeout(context.Background(), s.App().Conn().Config().CallTimeout())
			defer cancel()
			if err := s.setImages(ctx, sel, traceLogSpecsModified); err != nil {
				log.Error().Err(err).Msgf("PodSpec %s image update failed", sel)
			}*/
		s.App().Flash().Infof("trace log status updated successfully")
	})
	f.AddButton("Stop", func() {
		defer s.dismissDialog() //findLatestFile()
		scriptPath := s.FindTraceLogScript()
		startcmd := exec.Command("sh", scriptPath, "stop", podname, ns, podLabel)
		out, err := startcmd.CombinedOutput()
		if err != nil {
			s.App().Flash().Infof("trace log status closed fail!")
			return
		}
		ioutil.Discard.Write(out)
		s.App().Flash().Infof("trace log status updated successfully")
	})
	f.AddButton("Cancel", func() {
		s.dismissDialog()
	})
	return f, nil
}

func (s *ImageExtender) OpenTraceLog() {
	/*scriptReader := strings.NewReader(script)
	command := exec.Command("bash", "-s")
	command.Stdin = scriptReader
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// 执行命令
	if err := command.Run(); err != nil {
		fmt.Println("命令执行失败:", err)
		fmt.Println("错误输出:", stderr.String())
		return
	}

	// 输出结果
	fmt.Println("输出结果:", stdout.String())
	//s.App().Content.RemovePage(imageKey)*/
}

/*
func (s *ImageExtender) makeStyledForm() *tview.Form {
	f := tview.NewForm()
	f.SetItemPadding(0)
	f.SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tview.Styles.PrimitiveBackgroundColor).
		SetButtonTextColor(tview.Styles.PrimaryTextColor).
		SetLabelColor(tcell.ColorAqua).
		SetFieldTextColor(tcell.ColorOrange)
	return f
}

func (s *ImageExtender) getPodSpec(path string) (*corev1.PodSpec, error) {
	res, err := dao.AccessorFor(l.App().factory, l.GVR())
	if err != nil {
		return nil, err
	}
	resourceWPodSpec, ok := res.(dao.ContainsPodSpec)
	if !ok {
		return nil, fmt.Errorf("expecting a ContainsPodSpec for %q but got %T", l.GVR(), res)
	}

	return resourceWPodSpec.GetPodSpec(path)
}

func (s *ImageExtender) setTraceLogs(ctx context.Context, path string, imageSpecs dao.ImageSpecs) error {
	return nil
	res, err := dao.AccessorFor(l.App().factory, l.GVR())
	if err != nil {
		return err
	}

	resourceWPodSpec, ok := res.(dao.ContainsPodSpec)
	if !ok {
		return fmt.Errorf("expecting a scalable resource for %q", l.GVR())
	}

	return resourceWPodSpec.SetImages(ctx, path, imageSpecs)
}


func (s *ImageExtender) setImages(ctx context.Context, path string, imageSpecs dao.ImageSpecs) error {
	return nil
	res, err := dao.AccessorFor(l.App().factory, l.GVR())
	if err != nil {
		return err
	}

	resourceWPodSpec, ok := res.(dao.ContainsPodSpec)
	if !ok {
		return fmt.Errorf("expecting a scalable resource for %q", l.GVR())
	}

	return resourceWPodSpec.SetImages(ctx, path, imageSpecs)
}*/
