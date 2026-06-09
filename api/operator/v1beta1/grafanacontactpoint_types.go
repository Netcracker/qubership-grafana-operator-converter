package v1beta1

import (
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GrafanaContactPointSpec defines the desired state of GrafanaContactPoint
// +k8s:openapi-gen=true
type GrafanaContactPointSpec struct {
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +kubebuilder:default="10m"
	ResyncPeriod metav1.Duration `json:"resyncPeriod,omitempty"`

	// selects Grafanas for import
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	InstanceSelector *metav1.LabelSelector `json:"instanceSelector,omitempty"`

	// +optional
	DisableResolveMessage bool `json:"disableResolveMessage,omitempty"`

	// +kubebuilder:validation:type=string
	Name string `json:"name"`

	Settings *apiextensions.JSON `json:"settings"`

	// +kubebuilder:validation:Enum=alertmanager;prometheus-alertmanager;dingding;discord;email;googlechat;kafka;line;opsgenie;pagerduty;pushover;sensugo;sensu;slack;teams;telegram;threema;victorops;webhook;wecom;hipchat;oncall
	Type string `json:"type,omitempty"`

	// +optional
	AllowCrossNamespaceImport *bool `json:"allowCrossNamespaceImport,omitempty"`
}

// GrafanaContactPointStatus defines the observed state of GrafanaContactPoint
// +k8s:openapi-gen=true
type GrafanaContactPointStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []metav1.Condition `json:"conditions"`
}

// GrafanaContactPoint is the Schema for the grafanacontactpoints API
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type GrafanaContactPoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaContactPointSpec   `json:"spec,omitempty"`
	Status GrafanaContactPointStatus `json:"status,omitempty"`
}

// GrafanaContactPointList contains a list of GrafanaContactPoint
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GrafanaContactPointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrafanaContactPoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GrafanaContactPoint{}, &GrafanaContactPointList{})
}
