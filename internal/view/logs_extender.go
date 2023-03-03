package view

import (
	"fmt"

	"github.com/derailed/tcell/v2"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/ui"
)

/*This function returns a closure that handles a keyboard event for showing logs of the selected item in a table.

The LogsExtender struct seems to be a part of a larger application or package that involves displaying and interacting with Kubernetes resources.
GetTable method is used to retrieve a reference to a table widget that displays a list of resources.
The logsCmd method takes a boolean prev parameter that indicates whether to show the previous logs or the most recent ones.

When the closure is executed, it first retrieves the currently selected item from the table. If no item is selected, it returns nil.
If the selected item is not a valid Kubernetes resource path, it defaults to the current path of the table. Then it calls the showLogs method with the path and prev flag,
 which displays the logs of the selected resource in a separate view or pane. Finally, it returns nil to indicate that the event has been handled.
该函数返回一个处理键盘事件的闭包，用于显示表格中选定项目的日志。

LogsExtender结构似乎是一个更大的应用程序或包的一部分，涉及到显示和交互Kubernetes资源。GetTable方法用于检索对显示资源列表的表格小部件的引用。
logsCmd方法接收一个布尔参数prev，表示是显示以前的日志还是最近的日志。

当关闭被执行时，它首先从表中检索当前选择的项目。如果没有选择任何项目，它将返回nil。如果选择的项目不是有效的Kubernetes资源路径，它将默认为表的当前路径。
然后它调用带有路径和prev标志的showLogs方法，在一个单独的视图或窗格中显示所选资源的日志。最后，它返回nil以表示该事件已被处理。
*/
// LogsExtender adds log actions to a given viewer.
type LogsExtender struct {
	ResourceViewer

	optionsFn LogOptionsFunc
}

// NewLogsExtender returns a new extender.
func NewLogsExtender(v ResourceViewer, f LogOptionsFunc) ResourceViewer {
	l := LogsExtender{
		ResourceViewer: v,
		optionsFn:      f,
	}
	l.AddBindKeysFn(l.bindKeys)

	return &l
}

// BindKeys injects new menu actions.
func (l *LogsExtender) bindKeys(aa ui.KeyActions) {
	aa.Add(ui.KeyActions{
		ui.KeyL: ui.NewKeyAction("Logs", l.logsCmd(false), true),
		ui.KeyP: ui.NewKeyAction("Logs Previous", l.logsCmd(true), true),
	})
}

func (l *LogsExtender) logsCmd(prev bool) func(evt *tcell.EventKey) *tcell.EventKey {
	return func(evt *tcell.EventKey) *tcell.EventKey {
		path := l.GetTable().GetSelectedItem()
		_ = fmt.Errorf("Rider print %s. Please set the image on the controller", path)
		if path == "" {
			return nil
		}
		if !isResourcePath(path) {
			path = l.GetTable().Path
		}
		l.showLogs(path, prev)

		return nil
	}
}

func isResourcePath(p string) bool {
	ns, n := client.Namespaced(p)
	return ns != "" && n != ""
}

func (l *LogsExtender) showLogs(path string, prev bool) {
	ns, _ := client.Namespaced(path)
	_, err := l.App().factory.CanForResource(ns, "v1/pods", client.MonitorAccess)
	if err != nil {
		l.App().Flash().Err(err)
		return
	}
	opts := l.buildLogOpts(path, "", prev)
	if l.optionsFn != nil {
		if opts, err = l.optionsFn(prev); err != nil {
			l.App().Flash().Err(err)
			return
		}
	}
	if err := l.App().inject(NewLog(l.GVR(), opts), false); err != nil {
		l.App().Flash().Err(err)
	}
}

// buildLogOpts(path, co, prev, false, config.DefaultLoggerTailCount),.
func (l *LogsExtender) buildLogOpts(path, co string, prevLogs bool) *dao.LogOptions {
	cfg := l.App().Config.K9s.Logger
	opts := dao.LogOptions{
		Path:          path,
		Container:     co,
		Lines:         int64(cfg.TailCount),
		Previous:      prevLogs,
		ShowTimestamp: cfg.ShowTime,
	}
	if opts.Container == "" {
		opts.AllContainers = true
	}

	return &opts
}
