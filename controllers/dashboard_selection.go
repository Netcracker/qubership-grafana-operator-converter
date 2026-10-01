package controllers

import (
	"context"
	"fmt"

	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// dashboardSelected reports whether source matches the configured dashboard
// selectors. A nil or empty DashboardLabelSelector matches every dashboard.
// Any one DashboardLabelSelector entry is enough.
//
// In namespace-scoped mode, dashboardSelected evaluates DashboardLabelSelector
// only. In cluster-wide mode, a non-empty DashboardNamespaceSelector must also
// match the labels of the source Namespace. An error from that read is
// returned to the caller.
func (c *ConverterController) dashboardSelected(ctx context.Context, source *v1alpha1.GrafanaDashboard) (bool, error) {
	matched, err := dashboardLabelsMatch(c.ConverterConf.DashboardLabelSelector, source.GetLabels())
	if err != nil || !matched {
		return false, err
	}

	namespaces, err := getWatchNamespaces()
	if err != nil {
		return false, err
	}
	if len(namespaces) > 0 || c.ConverterConf.DashboardNamespaceSelector == nil {
		return true, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(c.ConverterConf.DashboardNamespaceSelector)
	if err != nil {
		return false, newPermanentDashboardError("invalid dashboardNamespaceSelector: %w", err)
	}
	if selector.Empty() {
		// An empty selector matches every namespace, so the Namespace is not read.
		return true, nil
	}
	if c.kubeClient == nil {
		// The client is fixed at startup, so retrying cannot attach it.
		return false, newPermanentDashboardError("cannot read Namespace %s: core client is not configured", source.Namespace)
	}
	namespace, err := c.kubeClient.CoreV1().Namespaces().Get(ctx, source.Namespace, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get Namespace %s: %w", source.Namespace, err)
	}
	return selector.Matches(labels.Set(namespace.Labels)), nil
}

// dashboardLabelsMatch reports whether objectLabels satisfy any selector.
// An empty selectors list matches every object. A nil entry matches every
// object.
func dashboardLabelsMatch(selectors []*metav1.LabelSelector, objectLabels map[string]string) (bool, error) {
	if len(selectors) == 0 {
		return true, nil
	}
	for _, raw := range selectors {
		selector, err := metav1.LabelSelectorAsSelector(raw)
		if err != nil {
			return false, newPermanentDashboardError("invalid dashboardLabelSelector: %w", err)
		}
		if selector.Empty() || selector.Matches(labels.Set(objectLabels)) {
			return true, nil
		}
	}
	return false, nil
}

// enqueueDashboardsInNamespace lists legacy GrafanaDashboards in the Namespace
// carried by object and enqueues each one. object is a *corev1.Namespace, or a
// [cache.DeletedFinalStateUnknown] whose object is a Namespace. An unexpected
// object is logged and not queued. A list error is logged and retried, because
// the Namespace informer does not repeat the event while resync is disabled.
func (c *ConverterController) enqueueDashboardsInNamespace(object any) {
	namespace, ok := namespaceFromEvent(object)
	if !ok {
		c.log.Error(fmt.Errorf("received %T", object), "Cannot enqueue GrafanaDashboards for Namespace: unexpected object type")
		return
	}
	if err := c.enqueueListedDashboards(c.ctx, namespace.Name); err != nil {
		c.log.Error(err, "Cannot list GrafanaDashboards for Namespace", "namespace", namespace.Name)
		c.dashboardQueue.Add(dashboardQueueItem{Namespace: namespace.Name})
	}
}

// enqueueListedDashboards lists legacy GrafanaDashboards in namespace and
// enqueues each one. A list error is returned so the caller can retry it.
func (c *ConverterController) enqueueListedDashboards(ctx context.Context, namespace string) error {
	dashboards, err := c.v1alpha1clientset.IntegreatlyV1alpha1().GrafanaDashboards(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list GrafanaDashboards in Namespace %s: %w", namespace, err)
	}
	for i := range dashboards.Items {
		dashboard := &dashboards.Items[i]
		c.dashboardQueue.Add(dashboardQueueItem{Namespace: dashboard.Namespace, Name: dashboard.Name})
	}
	return nil
}

// namespaceFromEvent returns the Namespace carried by a core informer event.
// A DeletedFinalStateUnknown tombstone is unwrapped. Any other object returns false.
func namespaceFromEvent(object any) (*corev1.Namespace, bool) {
	if namespace, ok := object.(*corev1.Namespace); ok {
		return namespace, true
	}
	tombstone, ok := object.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}
	namespace, ok := tombstone.Obj.(*corev1.Namespace)
	return namespace, ok
}

// deleteManagedDashboardTarget deletes the v1beta1 GrafanaDashboard named name
// in namespace when that object carries the converter ownership marker.
// The delete is limited to the object that was read, by UID and resource
// version. A conflict is returned so the caller can read the object again.
// It returns nil when the target is already absent. A target without the
// marker is left in place, and the error is permanent.
func (c *ConverterController) deleteManagedDashboardTarget(ctx context.Context, namespace, name string) error {
	targetClient := c.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDashboards(namespace)
	target, err := targetClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get target GrafanaDashboard: %w", err)
	}
	if !isConverterManaged(target) {
		return newPermanentDashboardError("target GrafanaDashboard %s/%s exists without the converter ownership marker", target.Namespace, target.Name)
	}
	uid := target.GetUID()
	resourceVersion := target.GetResourceVersion()
	err = targetClient.Delete(ctx, name, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID:             &uid,
			ResourceVersion: &resourceVersion,
		},
	})
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete target GrafanaDashboard: %w", err)
	}
	c.log.Info("Deleted target GrafanaDashboard", "name", name, "namespace", namespace)
	return nil
}
