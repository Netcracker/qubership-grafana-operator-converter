package controllers

import (
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	managedByOperatorLabelKey   = "app.kubernetes.io/managed-by-operator"
	managedByOperatorLabelValue = "grafana-operator-converter"
)

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

func isConverterManaged(object metav1.Object) bool {
	return object.GetLabels()[managedByOperatorLabelKey] == managedByOperatorLabelValue
}
