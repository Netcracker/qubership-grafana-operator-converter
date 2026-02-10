package v1beta1

import (
	"crypto/sha256"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GrafanaFolderSpec defines the desired state of GrafanaFolder
// +k8s:openapi-gen=true
type GrafanaFolderSpec struct {
	// +optional
	Title string `json:"title,omitempty"`

	// raw json with folder permissions
	// +optional
	Permissions string `json:"permissions,omitempty"`

	// selects Grafanas for import
	// +k8s:openapi-gen=true
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	InstanceSelector *metav1.LabelSelector `json:"instanceSelector,omitempty"`

	// allow to import this resources from an operator in a different namespace
	// +optional
	AllowCrossNamespaceImport *bool `json:"allowCrossNamespaceImport,omitempty"`

	// how often the folder is synced, defaults to 5m if not set
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +kubebuilder:default="5m"
	ResyncPeriod string `json:"resyncPeriod,omitempty"`
}

// GrafanaFolderStatus defines the observed state of GrafanaFolder
// +k8s:openapi-gen=true
type GrafanaFolderStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Hash string `json:"hash,omitempty"`
	// The folder instanceSelector can't find matching grafana instances
	NoMatchingInstances bool `json:"NoMatchingInstances,omitempty"`
	// Last time the folder was resynced
	LastResync metav1.Time `json:"lastResync,omitempty"`
}

// GrafanaFolder is the Schema for the grafanafolders API
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="No matching instances",type="boolean",JSONPath=".status.NoMatchingInstances",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type GrafanaFolder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaFolderSpec   `json:"spec,omitempty"`
	Status GrafanaFolderStatus `json:"status,omitempty"`
}

// GrafanaFolderList contains a list of GrafanaFolder
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GrafanaFolderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrafanaFolder `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GrafanaFolder{}, &GrafanaFolderList{})
}

func (in *GrafanaFolderList) Find(namespace string, name string) *GrafanaFolder {
	for _, folder := range in.Items {
		if folder.Namespace == namespace && folder.Name == name {
			return &folder
		}
	}
	return nil
}

func (in *GrafanaFolder) Hash() string {
	hash := sha256.New()
	hash.Write([]byte(in.Spec.Title))
	hash.Write([]byte(in.Spec.Permissions))
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (in *GrafanaFolder) Unchanged() bool {
	return in.Hash() == in.Status.Hash
}

func (in *GrafanaFolder) IsAllowCrossNamespaceImport() bool {
	if in.Spec.AllowCrossNamespaceImport != nil {
		return *in.Spec.AllowCrossNamespaceImport
	}
	return false
}

func (in *GrafanaFolder) GetTitle() string {
	if in.Spec.Title != "" {
		return in.Spec.Title
	}

	return in.Name
}

func (in *GrafanaFolder) GetResyncPeriod() time.Duration {
	if in.Spec.ResyncPeriod == "" {
		in.Spec.ResyncPeriod = DefaultResyncPeriod
		return in.GetResyncPeriod()
	}

	duration, err := time.ParseDuration(in.Spec.ResyncPeriod)
	if err != nil {
		in.Spec.ResyncPeriod = DefaultResyncPeriod
		return in.GetResyncPeriod()
	}

	return duration
}

func (in *GrafanaFolder) ResyncPeriodHasElapsed() bool {
	if in.Status.LastResync.IsZero() {
		return true
	}
	deadline := in.Status.LastResync.Add(in.GetResyncPeriod())
	return time.Now().After(deadline)
}
