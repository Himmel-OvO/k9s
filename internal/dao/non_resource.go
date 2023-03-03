package dao

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/derailed/k9s/internal/client"
)

/*
This is Go code that defines a struct called NonResource which represents a non Kubernetes resource. It contains a Factory field which is used to create new resources, a GVR field which is a client.GVR object that represents a Group/Version/Resource of the non Kubernetes resource, and a sync.RWMutex field for thread safety.

The Init function initializes the NonResource struct with the given Factory and GVR.

The GetFactory function returns the Factory associated with the NonResource object.

The GVR function returns a string representation of the GVR field of the NonResource object.

The Get function is not implemented and always returns an error with the message "NYI!", which stands for "Not Yet Implemented".
*/
// NonResource represents a non k8s resource.
type NonResource struct {
	Factory

	gvr client.GVR
	mx  sync.RWMutex
}

// Init initializes the resource.
func (n *NonResource) Init(f Factory, gvr client.GVR) {
	n.mx.Lock()
	{
		n.Factory, n.gvr = f, gvr
	}
	n.mx.Unlock()
}

func (n *NonResource) GetFactory() Factory {
	n.mx.RLock()
	defer n.mx.RUnlock()

	return n.Factory
}

// GVR returns a gvr.
func (n *NonResource) GVR() string {
	n.mx.RLock()
	defer n.mx.RUnlock()

	return n.gvr.String()
}

// Get returns the given resource.
func (n *NonResource) Get(context.Context, string) (runtime.Object, error) {
	return nil, fmt.Errorf("NYI!")
}
