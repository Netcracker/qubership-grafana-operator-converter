package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	v1alpha1clientset "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned"
	v1alpha1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned/fake"
	v1beta1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned/fake"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func TestDashboardWorkerRetriesTransientCreateError(t *testing.T) {
	source := testSourceDashboard("sample", "source-uid", "desired")
	alphaClient := v1alpha1fake.NewSimpleClientset(source)
	betaClient := v1beta1fake.NewSimpleClientset()
	var createAttempts atomic.Int32
	betaClient.PrependReactor("create", "grafanadashboards", func(k8stesting.Action) (bool, runtime.Object, error) {
		if createAttempts.Add(1) == 1 {
			return true, nil, errors.New("temporary create failure")
		}
		return false, nil, nil
	})
	controller := newDashboardTestController(alphaClient, betaClient)
	startDashboardTestWorker(t, controller)

	controller.enqueueSourceDashboard(source)

	require.Eventually(t, func() bool {
		_, err := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(source.Namespace).Get(
			context.Background(), source.Name, metav1.GetOptions{},
		)
		return err == nil
	}, time.Second, 5*time.Millisecond)
	assert.GreaterOrEqual(t, createAttempts.Load(), int32(2))
}

func TestDashboardWorkerRetriesTransientUpdateError(t *testing.T) {
	source := testSourceDashboard("sample", "source-uid", "desired")
	target := testTargetDashboard("sample", "source-uid", "drifted")
	alphaClient := v1alpha1fake.NewSimpleClientset(source)
	betaClient := v1beta1fake.NewSimpleClientset(target)
	var updateAttempts atomic.Int32
	betaClient.PrependReactor("update", "grafanadashboards", func(k8stesting.Action) (bool, runtime.Object, error) {
		if updateAttempts.Add(1) == 1 {
			return true, nil, errors.New("temporary update failure")
		}
		return false, nil, nil
	})
	controller := newDashboardTestController(alphaClient, betaClient)
	startDashboardTestWorker(t, controller)

	controller.enqueueSourceDashboard(source)

	require.Eventually(t, func() bool {
		actual, err := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(source.Namespace).Get(
			context.Background(), source.Name, metav1.GetOptions{},
		)
		return err == nil && actual.Spec.Json == "desired"
	}, time.Second, 5*time.Millisecond)
	assert.GreaterOrEqual(t, updateAttempts.Load(), int32(2))
}

func TestDeletedManagedTargetIsRecreatedWithoutSourceEvent(t *testing.T) {
	source := testSourceDashboard("sample", "source-uid", "desired")
	target := testTargetDashboard("sample", "source-uid", "desired")
	alphaClient := v1alpha1fake.NewSimpleClientset(source)
	betaClient := v1beta1fake.NewSimpleClientset(target)
	controller := newDashboardTestController(alphaClient, betaClient)
	startDashboardTestWorker(t, controller)

	require.NoError(t, betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(target.Namespace).Delete(
		context.Background(), target.Name, metav1.DeleteOptions{},
	))
	controller.enqueueDeletedTargetDashboard(target)

	require.Eventually(t, func() bool {
		actual, err := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(target.Namespace).Get(
			context.Background(), target.Name, metav1.GetOptions{},
		)
		return err == nil && actual.Spec.Json == "desired"
	}, time.Second, 5*time.Millisecond)
}

func TestModifiedManagedTargetIsRepairedWithoutSourceEvent(t *testing.T) {
	source := testSourceDashboard("sample", "source-uid", "desired")
	target := testTargetDashboard("sample", "source-uid", "desired")
	alphaClient := v1alpha1fake.NewSimpleClientset(source)
	betaClient := v1beta1fake.NewSimpleClientset(target)
	controller := newDashboardTestController(alphaClient, betaClient)
	startDashboardTestWorker(t, controller)

	drifted := target.DeepCopy()
	drifted.Spec.Json = "drifted"
	_, err := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(target.Namespace).Update(
		context.Background(), drifted, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	controller.enqueueUpdatedTargetDashboard(target, drifted)

	require.Eventually(t, func() bool {
		actual, getErr := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(target.Namespace).Get(
			context.Background(), target.Name, metav1.GetOptions{},
		)
		return getErr == nil && actual.Spec.Json == "desired"
	}, time.Second, 5*time.Millisecond)
}

func TestSourceDeletionOnlyDeletesMatchingManagedTarget(t *testing.T) {
	tests := []struct {
		name          string
		target        *v1beta1.GrafanaDashboard
		deletedUID    types.UID
		expectDeleted bool
	}{
		{
			name:          "matching managed target",
			target:        testTargetDashboard("sample", "source-uid", "desired"),
			deletedUID:    types.UID("source-uid"),
			expectDeleted: true,
		},
		{
			name:       "different source UID",
			target:     testTargetDashboard("sample", "new-source-uid", "desired"),
			deletedUID: types.UID("old-source-uid"),
		},
		{
			name: "unmanaged target",
			target: &v1beta1.GrafanaDashboard{
				ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: "product-a"},
				Spec:       v1beta1.GrafanaDashboardSpec{Json: "foreign"},
			},
			deletedUID: types.UID("source-uid"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			alphaClient := v1alpha1fake.NewSimpleClientset()
			betaClient := v1beta1fake.NewSimpleClientset(test.target)
			controller := newDashboardTestController(alphaClient, betaClient)

			err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{
				Namespace:        test.target.Namespace,
				Name:             test.target.Name,
				DeletedSourceUID: test.deletedUID,
			})

			require.NoError(t, err)
			_, getErr := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(test.target.Namespace).Get(
				context.Background(), test.target.Name, metav1.GetOptions{},
			)
			if test.expectDeleted {
				assert.True(t, apierrs.IsNotFound(getErr))
			} else {
				require.NoError(t, getErr)
			}
		})
	}
}

func TestStaleSourceDeletionDoesNotDeleteRecreatedSourceTarget(t *testing.T) {
	source := testSourceDashboard("sample", "new-source-uid", "desired")
	target := testTargetDashboard("sample", "new-source-uid", "desired")
	alphaClient := v1alpha1fake.NewSimpleClientset(source)
	betaClient := v1beta1fake.NewSimpleClientset(target)
	controller := newDashboardTestController(alphaClient, betaClient)

	err := controller.reconcileDashboard(context.Background(), dashboardQueueItem{
		Namespace:        source.Namespace,
		Name:             source.Name,
		DeletedSourceUID: types.UID("old-source-uid"),
	})

	require.NoError(t, err)
	actual, err := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(target.Namespace).Get(
		context.Background(), target.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "new-source-uid", actual.Annotations[grafanaDashboardSourceUIDAnnotation])
}

func TestPermanentSourceErrorWaitsForSourceChangeAndDoesNotBlockAnotherDashboard(t *testing.T) {
	invalidSource := testSourceDashboard("invalid", "invalid-uid", "")
	validSource := testSourceDashboard("valid", "valid-uid", "valid-content")
	alphaClient := v1alpha1fake.NewSimpleClientset(invalidSource, validSource)
	betaClient := v1beta1fake.NewSimpleClientset()
	var invalidGets atomic.Int32
	alphaClient.PrependReactor("get", "grafanadashboards", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.(k8stesting.GetAction).GetName() == invalidSource.Name {
			invalidGets.Add(1)
		}
		return false, nil, nil
	})
	controller := newDashboardTestController(alphaClient, betaClient)
	startDashboardTestWorker(t, controller)

	controller.enqueueSourceDashboard(invalidSource)
	controller.enqueueSourceDashboard(validSource)

	require.Eventually(t, func() bool {
		_, err := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(validSource.Namespace).Get(
			context.Background(), validSource.Name, metav1.GetOptions{},
		)
		return err == nil
	}, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return invalidGets.Load() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(4 * dashboardRetryInitialBackoff)
	assert.Equal(t, int32(1), invalidGets.Load())

	updatedSource := invalidSource.DeepCopy()
	updatedSource.Spec.Json = "fixed-content"
	_, err := alphaClient.IntegreatlyV1alpha1().GrafanaDashboards(updatedSource.Namespace).Update(
		context.Background(), updatedSource, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	controller.enqueueSourceDashboard(updatedSource)

	require.Eventually(t, func() bool {
		_, getErr := betaClient.GrafanaIntegreatlyV1beta1().GrafanaDashboards(updatedSource.Namespace).Get(
			context.Background(), updatedSource.Name, metav1.GetOptions{},
		)
		return getErr == nil
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, int32(2), invalidGets.Load())
}

func TestDashboardRetryBackoffIsExponentialAndCapped(t *testing.T) {
	limiter := newDashboardRateLimiter()
	item := dashboardQueueItem{Namespace: "product-a", Name: "sample"}

	assert.Equal(t, dashboardRetryInitialBackoff, limiter.When(item))
	assert.Equal(t, 2*dashboardRetryInitialBackoff, limiter.When(item))
	for range 32 {
		limiter.When(item)
	}
	assert.Equal(t, dashboardRetryMaximumBackoff, limiter.When(item))
}

func TestDashboardReconciliationHonorsCancellationAndAPITimeout(t *testing.T) {
	tests := []struct {
		name       string
		cancelRoot bool
	}{
		{name: "root cancellation", cancelRoot: true},
		{name: "API timeout"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestStarted := make(chan struct{})
			transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				close(requestStarted)
				<-request.Context().Done()
				return nil, request.Context().Err()
			})
			alphaClient, err := v1alpha1clientset.NewForConfig(&rest.Config{Host: "http://grafana-converter.test", Transport: transport})
			require.NoError(t, err)
			controller := &ConverterController{
				log:               logr.Discard(),
				v1alpha1clientset: alphaClient,
				v1beta1clientset:  v1beta1fake.NewSimpleClientset(),
			}
			controller.apiTimeout = 20 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- controller.reconcileDashboardWithTimeout(ctx, dashboardQueueItem{Namespace: "product-a", Name: "sample"})
			}()
			<-requestStarted
			if test.cancelRoot {
				cancel()
			}

			select {
			case err = <-done:
				require.Error(t, err)
				if test.cancelRoot {
					assert.ErrorIs(t, err, context.Canceled)
				} else {
					assert.ErrorIs(t, err, context.DeadlineExceeded)
				}
			case <-time.After(time.Second):
				t.Fatal("reconciliation did not stop after context cancellation")
			}
		})
	}
}

func TestConverterShutdownCancelsDashboardWorker(t *testing.T) {
	requestStarted := make(chan struct{})
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	alphaClient, err := v1alpha1clientset.NewForConfig(&rest.Config{Host: "http://grafana-converter.test", Transport: transport})
	require.NoError(t, err)
	controller := &ConverterController{
		log:               logr.Discard(),
		v1alpha1clientset: alphaClient,
		v1beta1clientset:  v1beta1fake.NewSimpleClientset(),
		dashboardQueue:    newDashboardQueue(),
		cacheSyncTimeout:  time.Second,
		apiTimeout:        time.Hour,
		v1alpha1InformerFactory: []scopedInformerFactory{{
			scope: "product-a",
			factory: &fakeInformerFactory{
				syncResults: map[reflect.Type]bool{reflect.TypeOf(v1alpha1.GrafanaDashboard{}): true},
			},
		}},
	}
	controller.setReadiness(errors.New("waiting for informer caches"))
	controller.dashboardQueue.Add(dashboardQueueItem{Namespace: "product-a", Name: "sample"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- controller.Start(ctx)
	}()
	<-requestStarted

	cancel()

	select {
	case startErr := <-done:
		require.NoError(t, startErr)
	case <-time.After(time.Second):
		t.Fatal("converter did not stop after context cancellation")
	}
}

func TestDeletedSourceDashboardHandlesTombstone(t *testing.T) {
	source := testSourceDashboard("sample", "source-uid", "desired")
	controller := newDashboardTestController(v1alpha1fake.NewSimpleClientset(), v1beta1fake.NewSimpleClientset())

	controller.enqueueDeletedSourceDashboard(cache.DeletedFinalStateUnknown{Key: "product-a/sample", Obj: source})
	item, shutdown := controller.dashboardQueue.Get()
	defer controller.dashboardQueue.Done(item)

	assert.False(t, shutdown)
	assert.Equal(t, source.ObjectMeta.UID, item.DeletedSourceUID)
}

func newDashboardTestController(
	alphaClient *v1alpha1fake.Clientset,
	betaClient *v1beta1fake.Clientset,
) *ConverterController {
	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.NewTypedItemExponentialFailureRateLimiter[dashboardQueueItem](time.Millisecond, 5*time.Millisecond),
	)
	return &ConverterController{
		log:               logr.Discard(),
		v1alpha1clientset: alphaClient,
		v1beta1clientset:  betaClient,
		dashboardQueue:    queue,
		apiTimeout:        time.Second,
	}
}

func startDashboardTestWorker(t *testing.T, controller *ConverterController) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.runDashboardWorker(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		controller.dashboardQueue.ShutDown()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("dashboard worker did not stop")
		}
	})
}

func testSourceDashboard(name string, uid types.UID, json string) *v1alpha1.GrafanaDashboard {
	return &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "product-a", UID: uid},
		Spec:       v1alpha1.GrafanaDashboardSpec{Json: json},
	}
}

func testTargetDashboard(name string, sourceUID types.UID, json string) *v1beta1.GrafanaDashboard {
	return &v1beta1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "product-a",
			UID:       types.UID(fmt.Sprintf("target-%s", name)),
			Labels:    map[string]string{managedByOperatorLabelKey: managedByOperatorLabelValue},
			Annotations: map[string]string{
				grafanaDashboardSourceUIDAnnotation: string(sourceUID),
			},
		},
		Spec: v1beta1.GrafanaDashboardSpec{
			Json:                      json,
			AllowCrossNamespaceImport: ptrTo(true),
			ResyncPeriod:              v1beta1.DefaultResyncPeriod,
		},
	}
}

func ptrTo[T any](value T) *T {
	return &value
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
