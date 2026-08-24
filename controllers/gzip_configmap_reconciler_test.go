package controllers

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned/fake"
	v1beta1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned/fake"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

func TestReconcileDashboardResolvesGzipConfigMapReference(t *testing.T) {
	compressed := gzipDashboardJSON(t, `{"title":"from-config-map"}`)
	source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
	configMap := testGzipConfigMap("dashboard-content", "dashboard.json.gz", compressed)
	alphaClient := v1alpha1fake.NewSimpleClientset(source)
	betaClient := v1beta1fake.NewSimpleClientset()
	controller := newDashboardTestController(alphaClient, betaClient)
	controller.coreClientset = kubefake.NewSimpleClientset(configMap)

	err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name})

	require.NoError(t, err)
	target, err := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(source.Namespace).Get(
		context.Background(), source.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, compressed, target.Spec.GzipJson)
	assert.Empty(t, target.Spec.Json)
}

func TestGzipConfigMapReferenceErrorsWaitForConfigMapEvent(t *testing.T) {
	validGzip := gzipDashboardJSON(t, `{"title":"valid"}`)
	invalidJSONGzip := gzipDashboardJSON(t, `not-json`)
	tests := []struct {
		name       string
		configMaps []runtime.Object
		errorMatch string
	}{
		{name: "missing ConfigMap", errorMatch: "does not exist"},
		{
			name:       "missing binaryData key",
			configMaps: []runtime.Object{testGzipConfigMap("dashboard-content", "different-key", validGzip)},
			errorMatch: "does not contain binaryData key",
		},
		{
			name:       "invalid gzip",
			configMaps: []runtime.Object{testGzipConfigMap("dashboard-content", "dashboard.json.gz", []byte("not-gzip"))},
			errorMatch: "does not contain valid gzip data",
		},
		{
			name:       "invalid JSON",
			configMaps: []runtime.Object{testGzipConfigMap("dashboard-content", "dashboard.json.gz", invalidJSONGzip)},
			errorMatch: "does not contain valid JSON",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
			alphaClient := v1alpha1fake.NewSimpleClientset(source)
			betaClient := v1beta1fake.NewSimpleClientset()
			controller := newDashboardTestController(alphaClient, betaClient)
			controller.coreClientset = kubefake.NewSimpleClientset(test.configMaps...)

			err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name})

			require.Error(t, err)
			assert.True(t, isPermanentDashboardError(err))
			assert.Contains(t, err.Error(), test.errorMatch)
			_, getErr := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(source.Namespace).Get(
				context.Background(), source.Name, metav1.GetOptions{},
			)
			assert.True(t, apierrs.IsNotFound(getErr))
		})
	}
}

func TestGzipConfigMapAPIFailureIsRetryable(t *testing.T) {
	source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
	controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(source), v1beta1fake.NewSimpleClientset())
	coreClient := kubefake.NewSimpleClientset()
	coreClient.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("temporary ConfigMap API failure")
	})
	controller.coreClientset = coreClient

	err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name})

	require.Error(t, err)
	assert.False(t, isPermanentDashboardError(err))
}

func TestGzipConfigMapReferenceRejectsInvalidConfigMapNameWithoutRetry(t *testing.T) {
	tests := []struct {
		name          string
		configMapName string
		errorMatch    string
	}{
		{name: "empty", errorMatch: "spec.gzipConfigMapRef.name must not be empty"},
		{name: "invalid DNS subdomain", configMapName: "Invalid_Name", errorMatch: "spec.gzipConfigMapRef.name"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := testGzipConfigMapSource("sample", "source-uid", test.configMapName, "dashboard.json.gz")
			coreClient := kubefake.NewSimpleClientset()
			controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(source), v1beta1fake.NewSimpleClientset())
			controller.coreClientset = coreClient

			err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name})

			require.Error(t, err)
			assert.True(t, isPermanentDashboardError(err))
			assert.Contains(t, err.Error(), test.errorMatch)
			assert.Empty(t, coreClient.Actions())
		})
	}
}

func TestGzipConfigMapReferenceRejectsInlineGzipJsonBeforeConfigMapGet(t *testing.T) {
	inlineGzip := gzipDashboardJSON(t, `{"title":"inline"}`)
	source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
	source.Spec.GzipJson = inlineGzip
	coreClient := kubefake.NewSimpleClientset(
		testGzipConfigMap("dashboard-content", "dashboard.json.gz", gzipDashboardJSON(t, `{"title":"from-config-map"}`)),
	)
	controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(source), v1beta1fake.NewSimpleClientset())
	controller.coreClientset = coreClient

	err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name})

	require.Error(t, err)
	assert.True(t, isPermanentDashboardError(err))
	assert.Contains(t, err.Error(), "cannot set both spec.gzipJson and spec.gzipConfigMapRef")
	assert.Empty(t, coreClient.Actions())
}

func TestGzipConfigMapReferenceRejectsOversizedDashboard(t *testing.T) {
	dashboardJSON := `{"title":"larger than the configured limit"}`
	compressed := gzipDashboardJSON(t, dashboardJSON)
	source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
	controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(source), v1beta1fake.NewSimpleClientset())
	controller.coreClientset = kubefake.NewSimpleClientset(
		testGzipConfigMap("dashboard-content", "dashboard.json.gz", compressed),
	)
	controller.gzipConfigMapMaxDecompressedSize = int64(len(dashboardJSON) - 1)

	err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name})

	require.Error(t, err)
	assert.True(t, isPermanentDashboardError(err))
	assert.Contains(t, err.Error(), "exceeds the")
	assert.Contains(t, err.Error(), "decompressed dashboard limit")
}

func TestGzipConfigMapReferenceUsesConvertedDashboardSourceValidation(t *testing.T) {
	compressed := gzipDashboardJSON(t, `{"title":"from-config-map"}`)
	source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
	source.Spec.Json = `{"title":"inline"}`
	controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(source), v1beta1fake.NewSimpleClientset())
	controller.coreClientset = kubefake.NewSimpleClientset(
		testGzipConfigMap("dashboard-content", "dashboard.json.gz", compressed),
	)

	err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name})

	require.Error(t, err)
	assert.True(t, isPermanentDashboardError(err))
	assert.Contains(t, err.Error(), "exactly one dashboard content source is required, found 2")
}

func TestConfigMapEventEnqueuesOnlyReferencingDashboards(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		gzipConfigMapReferenceIndex: indexDashboardByGzipConfigMapReference,
	})
	first := testGzipConfigMapSource("first", "first-uid", "shared-content", "first.json.gz")
	second := testGzipConfigMapSource("second", "second-uid", "shared-content", "second.json.gz")
	unrelated := testGzipConfigMapSource("unrelated", "unrelated-uid", "other-content", "dashboard.json.gz")
	require.NoError(t, indexer.Add(first))
	require.NoError(t, indexer.Add(second))
	require.NoError(t, indexer.Add(unrelated))
	controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(), v1beta1fake.NewSimpleClientset())
	configMap := testGzipConfigMap("shared-content", "first.json.gz", []byte("content"))

	controller.enqueueDashboardsForConfigMap(indexer, cache.DeletedFinalStateUnknown{Obj: configMap})

	require.Equal(t, 2, controller.dashboardQueue.Len())
	actualNames := make(map[string]struct{}, 2)
	for range 2 {
		item, shutdown := controller.dashboardQueue.Get()
		require.False(t, shutdown)
		actualNames[item.Name] = struct{}{}
		controller.dashboardQueue.Done(item)
		controller.dashboardQueue.Forget(item)
	}
	assert.Equal(t, map[string]struct{}{"first": {}, "second": {}}, actualNames)
}

func TestConfigMapMetadataEventEnqueuesReferencingDashboard(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		gzipConfigMapReferenceIndex: indexDashboardByGzipConfigMapReference,
	})
	source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
	require.NoError(t, indexer.Add(source))
	controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(), v1beta1fake.NewSimpleClientset())
	configMapMetadata := &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{Name: "dashboard-content", Namespace: "product-a"},
	}

	controller.enqueueDashboardsForConfigMap(indexer, configMapMetadata)

	require.Equal(t, 1, controller.dashboardQueue.Len())
	item, shutdown := controller.dashboardQueue.Get()
	defer controller.dashboardQueue.Done(item)
	assert.False(t, shutdown)
	assert.Equal(t, source.Name, item.Name)
}

func TestConfigMapEventReconcilesUpdatesWithoutSourceEvent(t *testing.T) {
	oldCompressed := gzipDashboardJSON(t, `{"title":"old"}`)
	newCompressed := gzipDashboardJSON(t, `{"title":"new"}`)
	source := testGzipConfigMapSource("sample", "source-uid", "dashboard-content", "dashboard.json.gz")
	configMap := testGzipConfigMap("dashboard-content", "dashboard.json.gz", oldCompressed)
	alphaClient := v1alpha1fake.NewSimpleClientset(source)
	betaClient := v1beta1fake.NewSimpleClientset()
	coreClient := kubefake.NewSimpleClientset(configMap)
	controller := newDashboardTestController(alphaClient, betaClient)
	controller.coreClientset = coreClient
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		gzipConfigMapReferenceIndex: indexDashboardByGzipConfigMapReference,
	})
	require.NoError(t, indexer.Add(source))
	require.NoError(t, controller.reconcileDashboard(
		context.Background(), dashboardQueueItem{Namespace: source.Namespace, Name: source.Name},
	))
	startDashboardTestWorker(t, controller)

	updatedConfigMap := configMap.DeepCopy()
	updatedConfigMap.BinaryData["dashboard.json.gz"] = newCompressed
	_, err := coreClient.CoreV1().ConfigMaps(configMap.Namespace).Update(
		context.Background(), updatedConfigMap, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	controller.enqueueDashboardsForConfigMap(indexer, updatedConfigMap)

	require.Eventually(t, func() bool {
		target, getErr := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(source.Namespace).Get(
			context.Background(), source.Name, metav1.GetOptions{},
		)
		return getErr == nil && bytes.Equal(target.Spec.GzipJson, newCompressed)
	}, 2*time.Second, 10*time.Millisecond)
}

func testGzipConfigMapSource(name string, uid types.UID, configMapName, key string) *v1alpha1.GrafanaDashboard {
	return &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "product-a", UID: uid},
		Spec: v1alpha1.GrafanaDashboardSpec{
			GzipConfigMapRef: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
				Key:                  key,
			},
		},
	}
}

func testGzipConfigMap(name, key string, value []byte) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "product-a"},
		BinaryData: map[string][]byte{key: value},
	}
}

func gzipDashboardJSON(t *testing.T, dashboardJSON string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(dashboardJSON))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}
