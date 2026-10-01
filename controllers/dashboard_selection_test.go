package controllers

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"
	"testing"

	v1alpha1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned/fake"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

// A namespace-scoped converter evaluates dashboard labels only. It does not
// read Namespace objects, including when DashboardNamespaceSelector is set.
func TestDashboardSelected(t *testing.T) {
	dashboardSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"dashboards": "platform"}}
	namespaceSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "platform"}}

	tests := []struct {
		name                string
		watchNamespace      string
		dashboardSelectors  []*metav1.LabelSelector
		namespaceSelector   *metav1.LabelSelector
		dashboardLabels     map[string]string
		namespaceLabels     map[string]string
		namespaceGetErr     error
		want                bool
		wantErr             bool
		wantPermanent       bool
		nilClient           bool
		forbidNamespaceRead bool
	}{
		{
			name:               "matching dashboard and namespace labels",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:  namespaceSelector,
			dashboardLabels:    map[string]string{"dashboards": "platform"},
			namespaceLabels:    map[string]string{"tenant": "platform"},
			want:               true,
		},
		{
			name:               "nonmatching dashboard labels",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:  namespaceSelector,
			dashboardLabels:    map[string]string{"dashboards": "product"},
			namespaceLabels:    map[string]string{"tenant": "platform"},
			want:               false,
		},
		{
			name:               "nonmatching namespace labels",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:  namespaceSelector,
			dashboardLabels:    map[string]string{"dashboards": "platform"},
			namespaceLabels:    map[string]string{"tenant": "product"},
			want:               false,
		},
		{
			name:                "namespace scoped ignores namespace labels",
			watchNamespace:      "product-a",
			dashboardSelectors:  []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:   namespaceSelector,
			dashboardLabels:     map[string]string{"dashboards": "platform"},
			namespaceLabels:     map[string]string{"tenant": "product"},
			want:                true,
			forbidNamespaceRead: true,
		},
		{
			name:                "namespace scoped nonmatching dashboard labels",
			watchNamespace:      "product-a",
			dashboardSelectors:  []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:   namespaceSelector,
			dashboardLabels:     map[string]string{"dashboards": "product"},
			namespaceLabels:     map[string]string{"tenant": "platform"},
			want:                false,
			forbidNamespaceRead: true,
		},
		{
			name:                "unset selectors select the dashboard",
			dashboardLabels:     map[string]string{"dashboards": "product"},
			namespaceLabels:     map[string]string{"tenant": "product"},
			want:                true,
			forbidNamespaceRead: true,
		},
		{
			name: "product selector matches when the platform selector does not",
			dashboardSelectors: []*metav1.LabelSelector{
				dashboardSelector,
				{MatchLabels: map[string]string{"dashboards": "product"}},
			},
			namespaceSelector: namespaceSelector,
			dashboardLabels:   map[string]string{"dashboards": "product"},
			namespaceLabels:   map[string]string{"tenant": "platform"},
			want:              true,
		},
		{
			name:               "namespace lookup failure is returned",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:  namespaceSelector,
			dashboardLabels:    map[string]string{"dashboards": "platform"},
			namespaceLabels:    map[string]string{"tenant": "platform"},
			namespaceGetErr:    errors.New("namespace lookup failed"),
			wantErr:            true,
		},
		{
			name:               "nil client with a namespace selector returns a permanent error",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:  namespaceSelector,
			dashboardLabels:    map[string]string{"dashboards": "platform"},
			nilClient:          true,
			wantPermanent:      true,
		},
		{
			name:               "nil client with an empty namespace selector selects the dashboard",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:  &metav1.LabelSelector{},
			dashboardLabels:    map[string]string{"dashboards": "platform"},
			nilClient:          true,
			want:               true,
		},
		{
			name:               "nil client without a namespace selector selects the dashboard",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			dashboardLabels:    map[string]string{"dashboards": "platform"},
			nilClient:          true,
			want:               true,
		},
		{
			name:               "namespace scoped nil client selects a matching dashboard",
			watchNamespace:     "product-a",
			dashboardSelectors: []*metav1.LabelSelector{dashboardSelector},
			namespaceSelector:  namespaceSelector,
			dashboardLabels:    map[string]string{"dashboards": "platform"},
			nilClient:          true,
			want:               true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(WatchNamespaceEnvVar, test.watchNamespace)
			source := testSourceDashboard("sample", "source-uid", "desired")
			source.Labels = test.dashboardLabels
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   source.Namespace,
				Labels: test.namespaceLabels,
			}}
			kubeClient := k8sfake.NewSimpleClientset(namespace)
			var namespaceReads atomic.Int32
			kubeClient.PrependReactor("*", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
				namespaceReads.Add(1)
				if test.namespaceGetErr != nil {
					return true, nil, test.namespaceGetErr
				}
				return false, nil, nil
			})
			controller := &ConverterController{
				log: logr.Discard(),
				ConverterConf: ConverterConfig{
					DashboardLabelSelector:     test.dashboardSelectors,
					DashboardNamespaceSelector: test.namespaceSelector,
				},
			}
			if !test.nilClient {
				controller.kubeClient = kubeClient
			}

			got, err := controller.dashboardSelected(context.Background(), source)

			if test.wantPermanent {
				require.Error(t, err)
				if !isPermanentDashboardError(err) {
					t.Errorf("dashboardSelected(%s) error = %v, want a permanent error", source.Name, err)
				}
				return
			}
			if test.wantErr {
				require.Error(t, err)
				if isPermanentDashboardError(err) {
					t.Errorf("dashboardSelected(%s) returned a permanent error: %v", source.Name, err)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got, "dashboardSelected(%s)", source.Name)
			if test.forbidNamespaceRead {
				assert.Zero(t, namespaceReads.Load(), "dashboardSelected(%s)", source.Name)
			}
		})
	}
}

// A Namespace event enqueues every legacy dashboard in that namespace.
// A DeletedFinalStateUnknown tombstone carries the same Namespace.
func TestNamespaceEventEnqueuesDashboardsInThatNamespace(t *testing.T) {
	selected := testSourceDashboard("selected", "selected-uid", "desired")
	alsoSelected := testSourceDashboard("also-selected", "also-selected-uid", "desired")
	other := testSourceDashboard("other", "other-uid", "desired")
	other.Namespace = "product-b"
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: selected.Namespace}}

	tests := []struct {
		name   string
		object any
	}{
		{name: "namespace object", object: namespace},
		{
			name:   "delete tombstone",
			object: cache.DeletedFinalStateUnknown{Key: selected.Namespace, Obj: namespace},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := newNamespaceEventController(t, selected, alsoSelected, other)

			controller.enqueueDashboardsInNamespace(test.object)

			assert.Equal(t, []dashboardQueueItem{
				{Namespace: "product-a", Name: "also-selected"},
				{Namespace: "product-a", Name: "selected"},
			}, queuedDashboardItems(controller), "enqueueDashboardsInNamespace(%s)", test.name)
		})
	}
}

func TestNamespaceEventWithUnexpectedObjectEnqueuesNoDashboards(t *testing.T) {
	selected := testSourceDashboard("selected", "selected-uid", "desired")
	tests := []struct {
		name   string
		object any
	}{
		{name: "unrelated object", object: &corev1.Pod{}},
		{
			name:   "tombstone with an unrelated object",
			object: cache.DeletedFinalStateUnknown{Key: selected.Namespace, Obj: &corev1.Pod{}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := newNamespaceEventController(t, selected)

			controller.enqueueDashboardsInNamespace(test.object)

			assert.Empty(t, queuedDashboardItems(controller), "enqueueDashboardsInNamespace(%s)", test.name)
		})
	}
}

func TestNamespaceEventWithNoDashboardsEnqueuesNothing(t *testing.T) {
	controller := newNamespaceEventController(t)

	controller.enqueueDashboardsInNamespace(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "product-a"}})

	assert.Empty(t, queuedDashboardItems(controller))
}

func TestNamespaceEventListFailureEnqueuesNoDashboards(t *testing.T) {
	selected := testSourceDashboard("selected", "selected-uid", "desired")
	alphaClient := v1alpha1fake.NewSimpleClientset(selected)
	alphaClient.PrependReactor("list", "grafanadashboards", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("list failed")
	})
	controller := &ConverterController{
		ctx:               context.Background(),
		log:               logr.Discard(),
		v1alpha1clientset: alphaClient,
		dashboardQueue:    newDashboardQueue(),
	}
	t.Cleanup(controller.dashboardQueue.ShutDown)

	controller.enqueueDashboardsInNamespace(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: selected.Namespace}})

	assert.Empty(t, queuedDashboardItems(controller), "enqueueDashboardsInNamespace(product-a)")
}

func newNamespaceEventController(t *testing.T, dashboards ...*v1alpha1.GrafanaDashboard) *ConverterController {
	t.Helper()
	objects := make([]runtime.Object, len(dashboards))
	for i, dashboard := range dashboards {
		objects[i] = dashboard
	}
	controller := &ConverterController{
		ctx:               context.Background(),
		log:               logr.Discard(),
		v1alpha1clientset: v1alpha1fake.NewSimpleClientset(objects...),
		dashboardQueue:    newDashboardQueue(),
	}
	t.Cleanup(controller.dashboardQueue.ShutDown)
	return controller
}

func queuedDashboardItems(controller *ConverterController) []dashboardQueueItem {
	items := make([]dashboardQueueItem, 0, controller.dashboardQueue.Len())
	for controller.dashboardQueue.Len() > 0 {
		item, shutdown := controller.dashboardQueue.Get()
		if shutdown {
			break
		}
		controller.dashboardQueue.Done(item)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
	return items
}
