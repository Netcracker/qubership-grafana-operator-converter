package controllers

import (
	"maps"
	"strings"

	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	managedByOperatorLabelKey   = "app.kubernetes.io/managed-by-operator"
	managedByOperatorLabelValue = "grafana-operator-converter"
	// convertedDashboardNameSuffix marks a v1beta1 GrafanaDashboard as a converter copy, so
	// its name cannot collide with a v1beta1 dashboard that another owner names like the source.
	convertedDashboardNameSuffix = "-converted"
)

// convertedDashboardName returns the name of the v1beta1 GrafanaDashboard converted from the
// legacy dashboard sourceName. A source name longer than 243 characters yields a name the API
// server rejects, and the reconciler reports that as a permanent error.
func convertedDashboardName(sourceName string) string {
	return sourceName + convertedDashboardNameSuffix
}

// sourceDashboardName returns the legacy dashboard name for a converted dashboard name.
// A name without the suffix, such as a copy from an earlier converter build, is returned as is.
func sourceDashboardName(convertedName string) string {
	return strings.TrimSuffix(convertedName, convertedDashboardNameSuffix)
}

func convertedObjectMeta(source metav1.Object, name string) metav1.ObjectMeta {
	labels := maps.Clone(source.GetLabels())
	if labels == nil {
		labels = make(map[string]string, 1)
	}
	labels[managedByOperatorLabelKey] = managedByOperatorLabelValue

	return metav1.ObjectMeta{
		Namespace:       source.GetNamespace(),
		Name:            name,
		Labels:          labels,
		Annotations:     maps.Clone(source.GetAnnotations()),
		OwnerReferences: append([]metav1.OwnerReference(nil), source.GetOwnerReferences()...),
	}
}

func convertedGrafanaDashboardMeta(source metav1.Object, deleteTargetOnSourceDeletion bool) metav1.ObjectMeta {
	labels := maps.Clone(source.GetLabels())
	if labels == nil {
		labels = make(map[string]string, 1)
	}
	for key := range labels {
		if key == "app.kubernetes.io/instance" || key == "app.kubernetes.io/managed-by" || strings.HasPrefix(key, "argocd.argoproj.io/") {
			delete(labels, key)
		}
	}
	labels[managedByOperatorLabelKey] = managedByOperatorLabelValue

	annotations := maps.Clone(source.GetAnnotations())
	for key := range annotations {
		if strings.HasPrefix(key, "meta.helm.sh/") || strings.HasPrefix(key, "argocd.argoproj.io/") {
			delete(annotations, key)
		}
	}
	delete(annotations, "helm.sh/resource-policy")
	delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
	// An empty map would never match the nil the API server returns for an object
	// without annotations, so the reconciler would keep issuing updates.
	if len(annotations) == 0 {
		annotations = nil
	}

	metadata := metav1.ObjectMeta{
		Namespace:   source.GetNamespace(),
		Name:        convertedDashboardName(source.GetName()),
		Labels:      labels,
		Annotations: annotations,
	}
	if deleteTargetOnSourceDeletion {
		metadata.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       v1alpha1.GrafanaDashboardKind,
			Name:       source.GetName(),
			UID:        source.GetUID(),
		}}
	}
	return metadata
}

func isConverterManaged(object metav1.Object) bool {
	return object.GetLabels()[managedByOperatorLabelKey] == managedByOperatorLabelValue
}
