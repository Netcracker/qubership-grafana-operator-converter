package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const GrafanaNotificationChannelKind = "GrafanaNotificationChannel"

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GrafanaNotificationChannelSpec defines the desired state of GrafanaNotificationChannel
// +k8s:openapi-gen=true
type GrafanaNotificationChannelSpec struct {
	Json string `json:"json"`
	Name string `json:"name"`
}

// GrafanaNotificationChannelStatus defines the observed state of GrafanaNotificationChannel
// +k8s:openapi-gen=true
type GrafanaNotificationChannelStatus struct {
	Phase   StatusPhase `json:"phase"`
	UID     string      `json:"uid"`
	ID      uint        `json:"id"`
	Message string      `json:"message"`
	Hash    string      `json:"hash"`
}

// GrafanaNotificationChannelRef Used to keep a notification channel reference without having access
// to the notification channel struct itself
type GrafanaNotificationChannelRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Hash      string `json:"hash"`
}

// GrafanaNotificationChannel is the Schema for the GrafanaNotificationChannels API
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type GrafanaNotificationChannel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaNotificationChannelSpec   `json:"spec,omitempty"`
	Status GrafanaNotificationChannelStatus `json:"status,omitempty"`
}

// GrafanaNotificationChannelList contains a list of GrafanaNotificationChannel
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GrafanaNotificationChannelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrafanaNotificationChannel `json:"items"`
}

type GrafanaNotificationChannelStatusMessage struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

func init() {
	SchemeBuilder.Register(&GrafanaNotificationChannel{}, &GrafanaNotificationChannelList{})
}
