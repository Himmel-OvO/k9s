package dao

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	mv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/render"
)

/*
DAO（数据访问对象）
实现了Pod中容器的数据访问接口，包括列表、日志等。

该文件包含了一个名为 Container 的类型，它实现了接口 Accessor 和 Loggable，其中 Accessor 是一种数据访问接口，Loggable 则是一种日志访问接口。

List 函数是 Accessor 接口的一个方法，它返回一个包含多个容器的运行时对象数组。函数首先获取上下文中传入的Pod名称，然后使用Pod名称获取Pod对象，从而遍历其Init容器和正常容器列表，为每个容器创建一个容器运行时对象。在创建容器运行时对象时，还会获取与该容器相关联的一些指标数据。返回结果是一个容器运行时对象数组。

TailLogs 函数是 Loggable 接口的一个方法，它返回一个用于跟踪给定容器日志的通道数组。该函数通过一个名为 Pod 的类型，它同样实现了 Loggable 接口，来获取日志。实际上，它将Pod的日志访问委托给 Pod 类型的 TailLogs 方法。

此文件还包含一些帮助函数，例如 makeContainerRes 用于创建容器运行时对象，getContainerStatus 用于获取容器的状态。此外还包含一个私有函数 fetchPod，该函数通过传入的Pod名称从Kubernetes API服务器获取Pod对象。
*/
var (
	_ Accessor = (*Container)(nil)
	_ Loggable = (*Container)(nil)
)

// Container represents a pod's container dao.
type Container struct {
	NonResource
}

// List returns a collection of containers.
func (c *Container) List(ctx context.Context, _ string) ([]runtime.Object, error) {
	fqn, ok := ctx.Value(internal.KeyPath).(string)
	if !ok {
		return nil, fmt.Errorf("no context path for %q", c.gvr)
	}

	var (
		cmx client.ContainersMetrics
		err error
	)
	if withMx, ok := ctx.Value(internal.KeyWithMetrics).(bool); withMx || !ok {
		cmx, _ = client.DialMetrics(c.Client()).FetchContainersMetrics(ctx, fqn)
	}

	po, err := c.fetchPod(fqn)
	if err != nil {
		return nil, err
	}
	res := make([]runtime.Object, 0, len(po.Spec.InitContainers)+len(po.Spec.Containers))
	for _, co := range po.Spec.InitContainers {
		res = append(res, makeContainerRes(co, po, cmx[co.Name], true))
	}
	for _, co := range po.Spec.Containers {
		res = append(res, makeContainerRes(co, po, cmx[co.Name], false))
	}

	return res, nil
}

// TailLogs tails a given container logs.
func (c *Container) TailLogs(ctx context.Context, opts *LogOptions) ([]LogChan, error) {
	po := Pod{}
	po.Init(c.Factory, client.NewGVR("v1/pods"))

	return po.TailLogs(ctx, opts)
}

// ----------------------------------------------------------------------------
// Helpers...

func makeContainerRes(co v1.Container, po *v1.Pod, cmx *mv1beta1.ContainerMetrics, isInit bool) render.ContainerRes {
	return render.ContainerRes{
		Container: &co,
		Status:    getContainerStatus(co.Name, po.Status),
		MX:        cmx,
		IsInit:    isInit,
		Age:       po.GetCreationTimestamp(),
	}
}

func getContainerStatus(co string, status v1.PodStatus) *v1.ContainerStatus {
	for _, c := range status.ContainerStatuses {
		if c.Name == co {
			return &c
		}
	}
	for _, c := range status.InitContainerStatuses {
		if c.Name == co {
			return &c
		}
	}

	return nil
}

func (c *Container) fetchPod(fqn string) (*v1.Pod, error) {
	o, err := c.GetFactory().Get("v1/pods", fqn, true, labels.Everything())
	if err != nil {
		return nil, err
	}
	var po v1.Pod
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.(*unstructured.Unstructured).Object, &po)
	return &po, err
}
