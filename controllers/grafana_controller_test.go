package controllers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	v1alpha1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned/fake"
	v1beta1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned/fake"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestNewGrafanaConverterControllerConfiguresInformerScopes(t *testing.T) {
	tests := []struct {
		name           string
		watchNamespace string
		expectedScopes []string
	}{
		{name: "cluster wide", expectedScopes: []string{"cluster-wide"}},
		{name: "explicit namespaces", watchNamespace: "product-a,product-b", expectedScopes: []string{"product-a", "product-b"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(WatchNamespaceEnvVar, test.watchNamespace)
			configPath := writeConverterConfig(t, "enable: true\ndashboard: true\ndatasource: true\nfolder: true\nnotification: true\n")

			controller, err := NewGrafanaConverterController(
				context.Background(),
				configPath,
				v1alpha1fake.NewSimpleClientset(),
				v1beta1fake.NewSimpleClientset(),
				nil,
				0,
				logr.Discard(),
			)

			require.NoError(t, err)
			assert.Equal(t, test.expectedScopes, controller.informerScopes())
			assert.Len(t, controller.v1beta1InformerFactory, len(test.expectedScopes))
			assert.NotNil(t, controller.dashboardQueue)
			readinessErr := controller.ReadinessCheck(nil)
			require.Error(t, readinessErr)
			for _, scope := range test.expectedScopes {
				assert.Contains(t, readinessErr.Error(), scope)
			}
		})
	}
}

// The core Namespace informer runs only for a cluster-wide watch when
// DashboardNamespaceSelector is set and dashboard conversion is enabled.
// An explicit WATCH_NAMESPACE, including a namespace named cluster-wide,
// does not register it. A nil core client in the cluster-wide case returns
// an error from the constructor.
func TestConverterRegistersNamespaceInformerForClusterWideSelection(t *testing.T) {
	// The copied Grafana fake clients do not opt out of watch-list semantics, so
	// an informer started over them waits for a bookmark the fake never sends.
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	const selectorConfig = `enable: true
dashboard: true
dashboardNamespaceSelector:
  matchLabels:
    tenant: platform
`
	const emptySelectorConfig = `enable: true
dashboard: true
dashboardNamespaceSelector: {}
`
	const datasourceConfig = `enable: true
datasource: true
dashboardNamespaceSelector:
  matchLabels:
    tenant: platform
`

	tests := []struct {
		name                  string
		watchNamespace        string
		config                string
		nilClient             bool
		wantNamespaceInformer bool
		wantClientErr         bool
	}{
		{
			name:                  "cluster-wide watch with a namespace selector",
			config:                selectorConfig,
			wantNamespaceInformer: true,
		},
		{
			name:                  "cluster-wide watch with an empty namespace selector",
			config:                emptySelectorConfig,
			wantNamespaceInformer: true,
		},
		{
			name:   "cluster-wide watch without a namespace selector",
			config: "enable: true\ndashboard: true\n",
		},
		{
			name:           "explicit namespace with a namespace selector",
			watchNamespace: "monitoring",
			config:         selectorConfig,
		},
		{
			name:           "namespace named cluster-wide with a namespace selector",
			watchNamespace: clusterWideScope,
			config:         selectorConfig,
		},
		{
			name:   "cluster-wide watch without dashboard conversion",
			config: datasourceConfig,
		},
		{
			name:          "cluster-wide watch with a namespace selector and no core client",
			config:        selectorConfig,
			nilClient:     true,
			wantClientErr: true,
		},
		{
			name:      "cluster-wide datasource conversion with a namespace selector and no core client",
			config:    datasourceConfig,
			nilClient: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(WatchNamespaceEnvVar, test.watchNamespace)
			alphaClient := v1alpha1fake.NewSimpleClientset()
			var kubeClient kubernetes.Interface
			if !test.nilClient {
				kubeClient = k8sfake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name:   "product-a",
					Labels: map[string]string{"tenant": "platform"},
				}})
			}

			controller, err := NewGrafanaConverterController(
				context.Background(),
				writeConverterConfig(t, test.config),
				alphaClient,
				v1beta1fake.NewSimpleClientset(),
				kubeClient,
				0,
				logr.Discard(),
			)

			if test.wantClientErr {
				require.ErrorContains(t, err, "core client is not configured")
				return
			}
			require.NoError(t, err)
			if controller.dashboardQueue != nil {
				t.Cleanup(controller.dashboardQueue.ShutDown)
			}
			assert.Equal(t, kubeClient, controller.kubeClient)
			assert.Equal(t, test.wantNamespaceInformer, namespaceInformerSynced(t, controller), test.name)
			assert.Equal(t, test.wantNamespaceInformer, listedDashboardsInNamespace(alphaClient, "product-a"),
				"list GrafanaDashboards in product-a")
		})
	}
}

func namespaceInformerSynced(t *testing.T, controller *ConverterController) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	for _, scopedFactory := range controller.informerFactories() {
		scopedFactory.factory.Start(ctx.Done())
	}
	namespaceType := reflect.TypeOf(&corev1.Namespace{})
	for _, scopedFactory := range controller.informerFactories() {
		for informerType, synced := range scopedFactory.factory.WaitForCacheSync(ctx.Done()) {
			if informerType == namespaceType && synced {
				return true
			}
		}
	}
	return false
}

func listedDashboardsInNamespace(client *v1alpha1fake.Clientset, namespace string) bool {
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "grafanadashboards" && action.GetNamespace() == namespace {
			return true
		}
	}
	return false
}

func TestNamespaceInformerUpdateEnqueuesDashboardsInThatNamespace(t *testing.T) {
	controller, kubeClient := startClusterWideNamespaceInformer(t)
	namespace, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), "product-a", metav1.GetOptions{})
	require.NoError(t, err)
	namespace.Labels["tenant"] = "other"
	_, err = kubeClient.CoreV1().Namespaces().Update(context.Background(), namespace, metav1.UpdateOptions{})
	require.NoError(t, err)

	assertProductADashboardsQueued(t, controller)
}

func TestNamespaceInformerDeleteEnqueuesDashboardsInThatNamespace(t *testing.T) {
	controller, kubeClient := startClusterWideNamespaceInformer(t)

	require.NoError(t, kubeClient.CoreV1().Namespaces().Delete(context.Background(), "product-a", metav1.DeleteOptions{}))

	assertProductADashboardsQueued(t, controller)
}

func startClusterWideNamespaceInformer(t *testing.T) (*ConverterController, *k8sfake.Clientset) {
	t.Helper()
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)
	t.Setenv(WatchNamespaceEnvVar, "")
	selected := testSourceDashboard("selected", "selected-uid", "desired")
	alsoSelected := testSourceDashboard("also-selected", "also-selected-uid", "desired")
	other := testSourceDashboard("other", "other-uid", "desired")
	other.Namespace = "product-b"
	kubeClient := k8sfake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "product-a",
		Labels: map[string]string{"tenant": "platform"},
	}})
	controller, err := NewGrafanaConverterController(
		context.Background(),
		writeConverterConfig(t, `enable: true
dashboard: true
dashboardNamespaceSelector:
  matchLabels:
    tenant: platform
`),
		v1alpha1fake.NewSimpleClientset(selected, alsoSelected, other),
		v1beta1fake.NewSimpleClientset(),
		kubeClient,
		0,
		logr.Discard(),
	)
	require.NoError(t, err)
	t.Cleanup(controller.dashboardQueue.ShutDown)
	require.True(t, namespaceInformerSynced(t, controller))
	queuedDashboardItems(controller)
	return controller, kubeClient
}

func assertProductADashboardsQueued(t *testing.T, controller *ConverterController) {
	t.Helper()
	want := []dashboardQueueItem{
		{Namespace: "product-a", Name: "also-selected"},
		{Namespace: "product-a", Name: "selected"},
	}
	require.Eventually(t, func() bool {
		return controller.dashboardQueue.Len() >= len(want)
	}, time.Second, 10*time.Millisecond, "enqueueDashboardsInNamespace(product-a)")
	assert.Equal(t, want, queuedDashboardItems(controller))
}

func TestNamespaceNamedClusterWideStaysNamespaceScoped(t *testing.T) {
	t.Setenv(WatchNamespaceEnvVar, clusterWideScope)
	configPath := writeConverterConfig(t, "enable: true\ndashboard: true\n")
	betaClient := v1beta1fake.NewSimpleClientset()

	controller, err := NewGrafanaConverterController(
		context.Background(),
		configPath,
		v1alpha1fake.NewSimpleClientset(),
		betaClient,
		nil,
		0,
		logr.Discard(),
	)

	require.NoError(t, err)
	require.Len(t, controller.v1beta1InformerFactory, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	controller.v1beta1InformerFactory[0].factory.Start(ctx.Done())
	controller.v1beta1InformerFactory[0].factory.WaitForCacheSync(ctx.Done())

	var observed bool
	for _, action := range betaClient.Actions() {
		if action.GetResource().Resource != "grafanadashboards" {
			continue
		}
		observed = true
		assert.Equal(t, clusterWideScope, action.GetNamespace(),
			"a namespace named %q must stay namespace scoped", clusterWideScope)
	}
	assert.True(t, observed, "the target dashboard informer must watch GrafanaDashboards")
}

func TestConverterReadinessFollowsInformerCacheSynchronization(t *testing.T) {
	firstFactory := &fakeInformerFactory{syncResults: map[reflect.Type]bool{reflect.TypeOf(v1alpha1.GrafanaDashboard{}): true}}
	secondFactory := &fakeInformerFactory{syncResults: map[reflect.Type]bool{reflect.TypeOf(v1alpha1.GrafanaDashboard{}): true}}
	controller := &ConverterController{
		log: logr.Discard(),
		v1alpha1InformerFactory: []scopedInformerFactory{
			{scope: "product-a", factory: firstFactory},
			{scope: "product-b", factory: secondFactory},
		},
	}
	controller.setReadiness(errors.New("waiting for informer caches"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- controller.Start(ctx)
	}()

	require.Eventually(t, func() bool {
		return firstFactory.wasStarted() && secondFactory.wasStarted() && controller.ReadinessCheck(nil) == nil
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}

func TestConverterReadinessReportsCacheSynchronizationFailure(t *testing.T) {
	failingFactory := &fakeInformerFactory{
		syncResults: map[reflect.Type]bool{reflect.TypeOf(v1alpha1.GrafanaDashboard{}): false},
		waitForStop: true,
	}
	healthyFactory := &fakeInformerFactory{syncResults: map[reflect.Type]bool{reflect.TypeOf(v1alpha1.GrafanaDashboard{}): true}}
	controller := &ConverterController{
		log:              logr.Discard(),
		cacheSyncTimeout: 10 * time.Millisecond,
		v1alpha1InformerFactory: []scopedInformerFactory{
			{scope: "product-a", factory: failingFactory},
			{scope: "product-b", factory: healthyFactory},
		},
	}
	controller.setReadiness(errors.New("waiting for informer caches"))

	err := controller.Start(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "product-a")
	assert.Contains(t, err.Error(), "GrafanaDashboard")
	assert.True(t, healthyFactory.wasStarted(), "all namespace informers must start before waiting for cache synchronization")
	assert.EqualError(t, controller.ReadinessCheck(nil), err.Error())
}

func TestReadConfigRejectsUnknownFields(t *testing.T) {
	path := writeConverterConfig(t, "enable: true\ndashboard: true\nunknownOption: true\n")

	_, err := ReadConfig(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknownOption")
}

func TestReadConfigRejectsEnabledConfigWithoutConverters(t *testing.T) {
	path := writeConverterConfig(t, "enable: true\n")

	_, err := ReadConfig(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one converter")
}

func TestReadConfigKeepsDashboardSelectors(t *testing.T) {
	path := writeConverterConfig(t, `enable: true
dashboard: true
instanceSelector:
  matchLabels:
    app.kubernetes.io/component: grafana
dashboardLabelSelector:
  - matchLabels:
      dashboards: platform
dashboardNamespaceSelector:
  matchLabels:
    tenant: platform
`)

	config, err := ReadConfig(path)

	require.NoError(t, err)
	assert.Equal(t, []*metav1.LabelSelector{{
		MatchLabels: map[string]string{"dashboards": "platform"},
	}}, config.DashboardLabelSelector)
	assert.Equal(t, &metav1.LabelSelector{
		MatchLabels: map[string]string{"tenant": "platform"},
	}, config.DashboardNamespaceSelector)
}

func TestReadConfigRejectsInvalidDashboardLabelSelector(t *testing.T) {
	path := writeConverterConfig(t, `enable: true
dashboard: true
dashboardLabelSelector:
  - matchExpressions:
      - key: dashboards
        operator: Invalid
`)

	_, err := ReadConfig(path)

	require.Error(t, err)
	assert.Regexp(t, "^invalid dashboardLabelSelector", err.Error())
}

func TestReadConfigRejectsInvalidDashboardNamespaceSelector(t *testing.T) {
	path := writeConverterConfig(t, `enable: true
dashboard: true
dashboardNamespaceSelector:
  matchExpressions:
    - key: tenant
      operator: Invalid
`)

	_, err := ReadConfig(path)

	require.Error(t, err)
	assert.Regexp(t, "^invalid dashboardNamespaceSelector", err.Error())
}

func TestReadConfigRejectsInvalidInstanceSelector(t *testing.T) {
	path := writeConverterConfig(t, `enable: true
dashboard: true
instanceSelector:
  matchExpressions:
    - key: app.kubernetes.io/component
      operator: In
      values: []
`)

	_, err := ReadConfig(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "instanceSelector")
}

func TestReadConfigRejectsMalformedYAML(t *testing.T) {
	path := writeConverterConfig(t, "enable: [\n")

	_, err := ReadConfig(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
}

func TestReadConfigAcceptsValidConfig(t *testing.T) {
	path := writeConverterConfig(t, "enable: true\ndashboard: true\ndeleteTargetOnSourceDeletion: true\n")

	config, err := ReadConfig(path)

	require.NoError(t, err)
	assert.True(t, config.Enable)
	assert.True(t, config.Dashboard)
	assert.True(t, config.DeleteTargetOnSourceDeletion)
}

func TestReadConfigKeepsMissingFileAsDisabledMode(t *testing.T) {
	config, err := ReadConfig(filepath.Join(t.TempDir(), "missing.yaml"))

	require.NoError(t, err)
	assert.Equal(t, ConverterConfig{}, *config)
}

func TestReadConfigRejectsUnreadablePath(t *testing.T) {
	_, err := ReadConfig(t.TempDir())

	require.Error(t, err)
}

func TestGetWatchNamespaces(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		expected   []string
		errorMatch string
	}{
		{name: "cluster wide"},
		{name: "single namespace", value: "monitoring", expected: []string{"monitoring"}},
		{name: "multiple namespaces", value: "monitoring,product-a", expected: []string{"monitoring", "product-a"}},
		{name: "invalid DNS label", value: "Monitoring", errorMatch: "Monitoring"},
		{name: "empty namespace", value: "monitoring,", errorMatch: "monitoring,"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(WatchNamespaceEnvVar, test.value)

			actual, err := getWatchNamespaces()

			if test.errorMatch != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.errorMatch)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func writeConverterConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parameters.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

type fakeInformerFactory struct {
	mu          sync.RWMutex
	started     bool
	syncResults map[reflect.Type]bool
	waitForStop bool
}

func (f *fakeInformerFactory) Start(<-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = true
}

func (f *fakeInformerFactory) WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool {
	if f.waitForStop {
		<-stopCh
	}
	return f.syncResults
}

func (f *fakeInformerFactory) wasStarted() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.started
}
