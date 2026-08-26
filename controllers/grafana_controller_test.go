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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
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
			metadataScheme := runtime.NewScheme()
			require.NoError(t, metav1.AddMetaToScheme(metadataScheme))

			controller, err := NewGrafanaConverterController(
				context.Background(),
				configPath,
				ConverterClients{
					V1alpha1: v1alpha1fake.NewSimpleClientset(),
					V1beta1:  v1beta1fake.NewSimpleClientset(),
					Core:     kubefake.NewSimpleClientset(),
					Metadata: metadatafake.NewSimpleMetadataClient(metadataScheme),
				},
				0,
				logr.Discard(),
			)

			require.NoError(t, err)
			assert.Equal(t, test.expectedScopes, controller.informerScopes())
			assert.Len(t, controller.v1beta1InformerFactory, len(test.expectedScopes))
			assert.Len(t, controller.configMapInformerFactory, len(test.expectedScopes))
			assert.NotNil(t, controller.dashboardQueue)
			assert.Equal(t, int64(defaultGzipConfigMapMaxDecompressedSize), controller.gzipConfigMapMaxDecompressedSize)
			readinessErr := controller.ReadinessCheck(nil)
			require.Error(t, readinessErr)
			for _, scope := range test.expectedScopes {
				assert.Contains(t, readinessErr.Error(), scope)
			}
		})
	}
}

func TestConfigMapInformerCachesOnlyMetadata(t *testing.T) {
	t.Setenv(WatchNamespaceEnvVar, "product-a")
	configPath := writeConverterConfig(t, "enable: true\ndashboard: true\n")
	metadataScheme := runtime.NewScheme()
	require.NoError(t, metav1.AddMetaToScheme(metadataScheme))
	configMapMetadata := &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "dashboard-content", Namespace: "product-a"},
	}
	controller, err := NewGrafanaConverterController(
		context.Background(),
		configPath,
		ConverterClients{
			V1alpha1: v1alpha1fake.NewSimpleClientset(),
			V1beta1:  v1beta1fake.NewSimpleClientset(),
			Core:     kubefake.NewSimpleClientset(),
			Metadata: metadatafake.NewSimpleMetadataClient(metadataScheme, configMapMetadata),
		},
		0,
		logr.Discard(),
	)
	require.NoError(t, err)
	require.Len(t, controller.configMapInformerFactory, 1)
	factory, ok := controller.configMapInformerFactory[0].factory.(*singleInformerFactory)
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	factory.Start(ctx.Done())
	syncResults := factory.WaitForCacheSync(ctx.Done())

	for _, synced := range syncResults {
		assert.True(t, synced)
	}
	require.Len(t, factory.informer.GetStore().List(), 1)
	assert.IsType(t, &metav1.PartialObjectMetadata{}, factory.informer.GetStore().List()[0])
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
	path := writeConverterConfig(t, "enable: true\ndashboard: true\ndeleteTargetOnSourceDeletion: true\ngzipConfigMapMaxDecompressedSize: 48Mi\n")

	config, err := ReadConfig(path)

	require.NoError(t, err)
	assert.True(t, config.Enable)
	assert.True(t, config.Dashboard)
	assert.True(t, config.DeleteTargetOnSourceDeletion)
	assert.Equal(t, "48Mi", config.GzipConfigMapMaxDecompressedSize)
}

func TestReadConfigRejectsInvalidGzipConfigMapSize(t *testing.T) {
	path := writeConverterConfig(t, "enable: true\ndashboard: true\ngzipConfigMapMaxDecompressedSize: unlimited\n")

	_, err := ReadConfig(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid gzipConfigMapMaxDecompressedSize")
}

func TestParseGzipConfigMapMaxDecompressedSize(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		expected   int64
		errorMatch string
	}{
		{name: "default", expected: defaultGzipConfigMapMaxDecompressedSize},
		{name: "configured", configured: "48Mi", expected: 48 * 1024 * 1024},
		{name: "invalid quantity", configured: "large", errorMatch: "invalid gzipConfigMapMaxDecompressedSize"},
		{name: "zero", configured: "0", errorMatch: "must be a positive whole number of bytes"},
		{name: "negative", configured: "-1Mi", errorMatch: "must be a positive whole number of bytes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := parseGzipConfigMapMaxDecompressedSize(test.configured)
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
