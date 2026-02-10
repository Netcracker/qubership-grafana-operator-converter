package v1beta1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GrafanaDatasourceInternal defines GrafanaDatasource configuration
// +k8s:openapi-gen=true
type GrafanaDatasourceInternal struct {
	UID           string `json:"uid,omitempty"`
	Name          string `json:"name,omitempty"`
	Type          string `json:"type,omitempty"`
	URL           string `json:"url,omitempty"`
	Access        string `json:"access,omitempty"`
	Database      string `json:"database,omitempty"`
	User          string `json:"user,omitempty"`
	IsDefault     *bool  `json:"isDefault,omitempty"`
	BasicAuth     *bool  `json:"basicAuth,omitempty"`
	BasicAuthUser string `json:"basicAuthUser,omitempty"`

	// Deprecated field, it has no effect
	OrgID *int64 `json:"orgId,omitempty"`
	// Deprecated field, it has no effect
	Editable *bool `json:"editable,omitempty"`

	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	JSONData json.RawMessage `json:"jsonData,omitempty"`

	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	SecureJSONData json.RawMessage `json:"secureJsonData,omitempty"`
}

// GrafanaDatasourceSpec defines the desired state of GrafanaDatasource
// +k8s:openapi-gen=true
type GrafanaDatasourceSpec struct {
	Datasource *GrafanaDatasourceInternal `json:"datasource"`

	// selects Grafana instances for import
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	InstanceSelector *metav1.LabelSelector `json:"instanceSelector,omitempty"`

	// plugins
	// +optional
	Plugins PluginList `json:"plugins,omitempty"`

	// environments variables from secrets or config maps
	// +optional
	ValuesFrom []GrafanaDatasourceValueFrom `json:"valuesFrom,omitempty"`

	// how often the datasource is refreshed, defaults to 5m if not set
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +kubebuilder:default="5m"
	ResyncPeriod string `json:"resyncPeriod,omitempty"`

	// allow to import this resources from an operator in a different namespace
	// +optional
	AllowCrossNamespaceImport *bool `json:"allowCrossNamespaceImport,omitempty"`
}

// GrafanaDatasourceValueFrom defines the environments variables from secrets or config maps
// +k8s:openapi-gen=true
type GrafanaDatasourceValueFrom struct {
	TargetPath string                           `json:"targetPath"`
	ValueFrom  GrafanaDatasourceValueFromSource `json:"valueFrom"`
}

// GrafanaDatasourceValueFromSource defines the environments variables from secrets or config maps
// +k8s:openapi-gen=true
type GrafanaDatasourceValueFromSource struct {
	// Selects a key of a ConfigMap.
	// +optional
	ConfigMapKeyRef *v1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	// Selects a key of a Secret.
	// +optional
	SecretKeyRef *v1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

// GrafanaDatasourceStatus defines the observed state of GrafanaDatasource
// +k8s:openapi-gen=true
type GrafanaDatasourceStatus struct {
	Hash        string `json:"hash,omitempty"`
	LastMessage string `json:"lastMessage,omitempty"`
	// The datasource instanceSelector can't find matching grafana instances
	NoMatchingInstances bool `json:"NoMatchingInstances,omitempty"`
	// Last time the datasource was resynced
	LastResync metav1.Time `json:"lastResync,omitempty"`
	UID        string      `json:"uid,omitempty"`
}

// GrafanaDatasource is the Schema for the grafanadatasources API
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="No matching instances",type="boolean",JSONPath=".status.NoMatchingInstances",description=""
// +kubebuilder:printcolumn:name="Last resync",type="date",format="date-time",JSONPath=".status.lastResync",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type GrafanaDatasource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaDatasourceSpec   `json:"spec,omitempty"`
	Status GrafanaDatasourceStatus `json:"status,omitempty"`
}

// GrafanaDatasourceList contains a list of GrafanaDatasource
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GrafanaDatasourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrafanaDatasource `json:"items"`
}

func (in *GrafanaDatasource) GetResyncPeriod() time.Duration {
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

func (in *GrafanaDatasource) ResyncPeriodHasElapsed() bool {
	if in.Status.LastResync.IsZero() {
		return true
	}
	deadline := in.Status.LastResync.Add(in.GetResyncPeriod())
	return time.Now().After(deadline)
}

func (in *GrafanaDatasource) Unchanged(hash string) bool {
	return in.Status.Hash == hash
}

func (in *GrafanaDatasource) IsUpdatedUID(uid string) bool {
	// Datasource has just been created, status is not yet updated
	if in.Status.UID == "" {
		return false
	}

	if uid == "" {
		uid = string(in.UID)
	}

	return in.Status.UID != uid
}

func (in *GrafanaDatasource) ExpandVariables(variables map[string][]byte) ([]byte, error) {
	if in.Spec.Datasource == nil {
		return nil, errors.New("data source is empty, can't expand variables")
	}

	raw, err := json.Marshal(in.Spec.Datasource)
	if err != nil {
		return nil, err
	}

	for key, value := range variables {
		patterns := []string{fmt.Sprintf("$%v", key), fmt.Sprintf("${%v}", key)}
		for _, pattern := range patterns {
			raw = bytes.ReplaceAll(raw, []byte(pattern), value)
		}
	}

	return raw, nil
}

func (in *GrafanaDatasource) IsAllowCrossNamespaceImport() bool {
	if in.Spec.AllowCrossNamespaceImport != nil {
		return *in.Spec.AllowCrossNamespaceImport
	}
	return false
}

func (in *GrafanaDatasourceList) Find(namespace string, name string) *GrafanaDatasource {
	for _, datasource := range in.Items {
		if datasource.Namespace == namespace && datasource.Name == name {
			return &datasource
		}
	}
	return nil
}

func init() {
	SchemeBuilder.Register(&GrafanaDatasource{}, &GrafanaDatasourceList{})
}
