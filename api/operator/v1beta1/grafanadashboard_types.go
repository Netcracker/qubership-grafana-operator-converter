package v1beta1

import (
	"bytes"
	"compress/gzip"
	"io"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DashboardSourceType string

const (
	DashboardSourceTypeRawJson    DashboardSourceType = "json"
	DashboardSourceTypeGzipJson   DashboardSourceType = "gzipJson"
	DashboardSourceJsonnetProject DashboardSourceType = "jsonnetProjectWithRuntimeRaw"
	DashboardSourceTypeUrl        DashboardSourceType = "url"
	DashboardSourceTypeJsonnet    DashboardSourceType = "jsonnet"
	DashboardSourceTypeGrafanaCom DashboardSourceType = "grafana"
	DashboardSourceConfigMap      DashboardSourceType = "configmap"
	DefaultResyncPeriod                               = "2m"
	DefaultResyncPeriodDuration   time.Duration       = 120000000000
)

// GrafanaDashboardDatasource defines datasource parameters.
// +k8s:openapi-gen=true
type GrafanaDashboardDatasource struct {
	InputName      string `json:"inputName"`
	DatasourceName string `json:"datasourceName"`
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GrafanaDashboardSpec defines the desired state of GrafanaDashboard
// +k8s:openapi-gen=true
type GrafanaDashboardSpec struct {
	// dashboard json
	// +optional
	Json string `json:"json,omitempty"`

	// GzipJson the dashboard's JSON compressed with Gzip. Base64-encoded when in YAML.
	// +optional
	GzipJson []byte `json:"gzipJson,omitempty"`

	// dashboard url
	// +optional
	Url string `json:"url,omitempty"`

	// Jsonnet
	// +optional
	Jsonnet string `json:"jsonnet,omitempty"`

	// Jsonnet project build
	JsonnetProjectBuild *JsonnetProjectBuild `json:"jsonnetLib,omitempty"`

	// grafana.com/dashboards
	// +optional
	GrafanaCom *GrafanaComDashboardReference `json:"grafanaCom,omitempty"`

	// dashboard from configmap
	// +optional
	ConfigMapRef *v1.ConfigMapKeySelector `json:"configMapRef,omitempty"`

	// selects Grafanas for import
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	InstanceSelector *metav1.LabelSelector `json:"instanceSelector,omitempty"`

	// folder assignment for dashboard
	// +optional
	FolderTitle string `json:"folder,omitempty"`

	// plugins
	// +optional
	Plugins PluginList `json:"plugins,omitempty"`

	// Cache duration for dashboards fetched from URLs
	// +optional
	ContentCacheDuration metav1.Duration `json:"contentCacheDuration,omitempty"`

	// how often the dashboard is refreshed, defaults to 5m if not set
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +kubebuilder:default="5m"
	ResyncPeriod string `json:"resyncPeriod,omitempty"`

	// maps required data sources to existing ones
	// +optional
	Datasources []GrafanaDashboardDatasource `json:"datasources,omitempty"`

	// allow to import this resources from an operator in a different namespace
	// +optional
	AllowCrossNamespaceImport *bool `json:"allowCrossNamespaceImport,omitempty"`

	// environments variables as a map
	// +optional
	Envs []GrafanaDashboardEnv `json:"envs,omitempty"`

	// environments variables from secrets or config maps
	// +optional
	EnvsFrom []GrafanaDashboardEnvFromSource `json:"envFrom,omitempty"`
}

// GrafanaDashboardEnv defines the environments variables as a map
// +k8s:openapi-gen=true
type GrafanaDashboardEnv struct {
	Name string `json:"name"`
	// Inline evn value
	// +optional
	Value string `json:"value,omitempty"`
	// Reference on value source, might be the reference on a secret or config map
	// +optional
	ValueFrom GrafanaDashboardEnvFromSource `json:"valueFrom,omitempty"`
}

// GrafanaDashboardEnvFromSource defines the environments variables from secrets or config maps
// +k8s:openapi-gen=true
type GrafanaDashboardEnvFromSource struct {
	// Selects a key of a ConfigMap.
	// +optional
	ConfigMapKeyRef *v1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	// Selects a key of a Secret.
	// +optional
	SecretKeyRef *v1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

// JsonnetProjectBuild defines the Jsonnet project build
// +k8s:openapi-gen=true
type JsonnetProjectBuild struct {
	JPath              []string `json:"jPath,omitempty"`
	FileName           string   `json:"fileName"`
	GzipJsonnetProject []byte   `json:"gzipJsonnetProject"`
}

// GrafanaComDashbooardReference is a reference to a dashboard on grafana.com/dashboards
// +k8s:openapi-gen=true
type GrafanaComDashboardReference struct {
	Id       int  `json:"id"`
	Revision *int `json:"revision,omitempty"`
}

// GrafanaDashboardStatus defines the observed state of GrafanaDashboard
// +k8s:openapi-gen=true
type GrafanaDashboardStatus struct {
	ContentCache     []byte      `json:"contentCache,omitempty"`
	ContentTimestamp metav1.Time `json:"contentTimestamp,omitempty"`
	ContentUrl       string      `json:"contentUrl,omitempty"`
	Hash             string      `json:"hash,omitempty"`
	// The dashboard instanceSelector can't find matching grafana instances
	NoMatchingInstances bool `json:"NoMatchingInstances,omitempty"`
	// Last time the dashboard was resynced
	LastResync metav1.Time `json:"lastResync,omitempty"`
	UID        string      `json:"uid,omitempty"`
}

// GrafanaDashboard is the Schema for the grafanadashboards API
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="No matching instances",type="boolean",JSONPath=".status.NoMatchingInstances",description=""
// +kubebuilder:printcolumn:name="Last resync",type="date",format="date-time",JSONPath=".status.lastResync",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type GrafanaDashboard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaDashboardSpec   `json:"spec,omitempty"`
	Status GrafanaDashboardStatus `json:"status,omitempty"`
}

// GrafanaDashboardList contains a list of GrafanaDashboard
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GrafanaDashboardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrafanaDashboard `json:"items"`
}

func (in *GrafanaDashboard) Unchanged(hash string) bool {
	return in.Status.Hash == hash
}

func (in *GrafanaDashboard) ResyncPeriodHasElapsed() bool {
	if in.Status.LastResync.IsZero() {
		return true
	}
	deadline := in.Status.LastResync.Add(in.GetResyncPeriod())
	return time.Now().After(deadline)
}

func (in *GrafanaDashboard) GetResyncPeriod() time.Duration {
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

func (in *GrafanaDashboard) GetSourceTypes() []DashboardSourceType {
	var sourceTypes []DashboardSourceType

	if in.Spec.Json != "" {
		sourceTypes = append(sourceTypes, DashboardSourceTypeRawJson)
	}

	if in.Spec.GzipJson != nil {
		sourceTypes = append(sourceTypes, DashboardSourceTypeGzipJson)
	}

	if in.Spec.Url != "" {
		sourceTypes = append(sourceTypes, DashboardSourceTypeUrl)
	}

	if in.Spec.Jsonnet != "" {
		sourceTypes = append(sourceTypes, DashboardSourceTypeJsonnet)
	}

	if in.Spec.GrafanaCom != nil {
		sourceTypes = append(sourceTypes, DashboardSourceTypeGrafanaCom)
	}

	if in.Spec.ConfigMapRef != nil {
		sourceTypes = append(sourceTypes, DashboardSourceConfigMap)
	}

	if in.Spec.JsonnetProjectBuild != nil {
		sourceTypes = append(sourceTypes, DashboardSourceJsonnetProject)
	}

	return sourceTypes
}

func (in *GrafanaDashboard) GetContentCache() []byte {
	return in.Status.getContentCache(in.Spec.Url, in.Spec.ContentCacheDuration.Duration)
}

// getContentCache returns content cache when the following conditions are met: url is the same, data is not expired, gzipped data is not corrupted
func (in *GrafanaDashboardStatus) getContentCache(url string, cacheDuration time.Duration) []byte {
	if in.ContentUrl != url {
		return []byte{}
	}

	notExpired := cacheDuration <= 0 || in.ContentTimestamp.Add(cacheDuration).After(time.Now())
	if !notExpired {
		return []byte{}
	}

	cache, err := Gunzip(in.ContentCache)
	if err != nil {
		return []byte{}
	}

	return cache
}

func (in *GrafanaDashboard) IsAllowCrossNamespaceImport() bool {
	if in.Spec.AllowCrossNamespaceImport != nil {
		return *in.Spec.AllowCrossNamespaceImport
	}
	return false
}

func (in *GrafanaDashboard) IsUpdatedUID(uid string) bool {
	// Dashboard has just been created, status is not yet updated
	if in.Status.UID == "" {
		return false
	}

	if uid == "" {
		uid = string(in.ObjectMeta.UID)
	}

	return in.Status.UID != uid
}

func Gunzip(compressed []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}

	return io.ReadAll(gz)
}

func Gzip(content []byte) ([]byte, error) {
	buf := bytes.NewBuffer([]byte{})
	gz := gzip.NewWriter(buf)

	_, err := gz.Write(content)
	if err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return io.ReadAll(buf)
}

func (in *GrafanaDashboardList) Find(namespace string, name string) *GrafanaDashboard {
	for _, dashboard := range in.Items {
		if dashboard.ObjectMeta.Namespace == namespace && dashboard.ObjectMeta.Name == name {
			return &dashboard
		}
	}
	return nil
}

func init() {
	SchemeBuilder.Register(&GrafanaDashboard{}, &GrafanaDashboardList{})
}
