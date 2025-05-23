// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	monitoringv1 "github.com/scylladb/scylla-operator/pkg/externalapi/monitoring/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
)

// PodMonitorLister helps list PodMonitors.
// All objects returned here must be treated as read-only.
type PodMonitorLister interface {
	// List lists all PodMonitors in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*monitoringv1.PodMonitor, err error)
	// PodMonitors returns an object that can list and get PodMonitors.
	PodMonitors(namespace string) PodMonitorNamespaceLister
	PodMonitorListerExpansion
}

// podMonitorLister implements the PodMonitorLister interface.
type podMonitorLister struct {
	listers.ResourceIndexer[*monitoringv1.PodMonitor]
}

// NewPodMonitorLister returns a new PodMonitorLister.
func NewPodMonitorLister(indexer cache.Indexer) PodMonitorLister {
	return &podMonitorLister{listers.New[*monitoringv1.PodMonitor](indexer, monitoringv1.Resource("podmonitor"))}
}

// PodMonitors returns an object that can list and get PodMonitors.
func (s *podMonitorLister) PodMonitors(namespace string) PodMonitorNamespaceLister {
	return podMonitorNamespaceLister{listers.NewNamespaced[*monitoringv1.PodMonitor](s.ResourceIndexer, namespace)}
}

// PodMonitorNamespaceLister helps list and get PodMonitors.
// All objects returned here must be treated as read-only.
type PodMonitorNamespaceLister interface {
	// List lists all PodMonitors in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*monitoringv1.PodMonitor, err error)
	// Get retrieves the PodMonitor from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*monitoringv1.PodMonitor, error)
	PodMonitorNamespaceListerExpansion
}

// podMonitorNamespaceLister implements the PodMonitorNamespaceLister
// interface.
type podMonitorNamespaceLister struct {
	listers.ResourceIndexer[*monitoringv1.PodMonitor]
}
