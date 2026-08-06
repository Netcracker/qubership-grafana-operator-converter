package controllers

import (
	"context"
	"errors"
	"testing"

	v1beta1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned/fake"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
)

const (
	converterManagedLabel = "app.kubernetes.io/managed-by-operator"
	converterManagedValue = "grafana-operator-converter"
)

func TestConvertGrafanaDashboardMarksManagedCopyWithoutMutatingSource(t *testing.T) {
	sourceLabels := map[string]string{"product": "sample"}
	source := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-dashboard",
			Namespace: "product-a",
			Labels:    sourceLabels,
		},
	}
	controller := &ConverterController{log: logr.Discard()}

	converted := controller.convertGrafanaDashboard(source)

	assert.Equal(t, converterManagedValue, converted.Labels[converterManagedLabel])
	assert.Equal(t, "sample", converted.Labels["product"])
	assert.Equal(t, map[string]string{"product": "sample"}, source.Labels)
}

func TestConvertGrafanaDatasourceMarksManagedCopyWithoutMutatingSource(t *testing.T) {
	source := &v1alpha1.GrafanaDataSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-datasource",
			Namespace: "product-a",
			Labels:    map[string]string{"product": "sample"},
		},
		Spec: v1alpha1.GrafanaDataSourceSpec{
			Datasources: []v1alpha1.GrafanaDataSourceFields{{Name: "VictoriaMetrics", Type: "prometheus"}},
		},
	}
	controller := &ConverterController{log: logr.Discard()}

	converted, err := controller.convertGrafanaDatasource(source)

	require.NoError(t, err)
	require.Len(t, converted, 1)
	assert.Equal(t, converterManagedValue, converted[0].Labels[converterManagedLabel])
	assert.Equal(t, map[string]string{"product": "sample"}, source.Labels)
}

func TestConvertGrafanaFolderMarksManagedCopyWithoutMutatingSource(t *testing.T) {
	source := &v1alpha1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-folder",
			Namespace: "product-a",
			Labels:    map[string]string{"product": "sample"},
		},
	}
	controller := &ConverterController{log: logr.Discard()}

	converted := controller.convertGrafanaFolder(source)

	assert.Equal(t, converterManagedValue, converted.Labels[converterManagedLabel])
	assert.Equal(t, map[string]string{"product": "sample"}, source.Labels)
}

func TestConvertGrafanaNotificationChannelMarksManagedCopyWithoutMutatingSource(t *testing.T) {
	source := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-contact-point",
			Namespace: "product-a",
			Labels:    map[string]string{"product": "sample"},
		},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"sample","type":"email","settings":{}}`,
		},
	}
	controller := &ConverterController{log: logr.Discard()}

	converted, err := controller.convertGrafanaNotificationChannel(source)

	require.NoError(t, err)
	assert.Equal(t, converterManagedValue, converted.Labels[converterManagedLabel])
	assert.Equal(t, map[string]string{"product": "sample"}, source.Labels)
}

func TestCreateGrafanaDashboardDoesNotAdoptUnmarkedCollision(t *testing.T) {
	existing := &v1beta1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{Name: "sample-dashboard", Namespace: "product-a"},
		Spec:       v1beta1.GrafanaDashboardSpec{Json: "foreign"},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
		Spec:       v1alpha1.GrafanaDashboardSpec{Json: "converted"},
	}

	controller.createGrafanaDashboard(source)

	actual, err := client.GrafanaIntegreatlyV1beta1().GrafanaDashboards(existing.Namespace).Get(
		context.Background(), existing.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "foreign", actual.Spec.Json)
	assert.NotContains(t, actual.Labels, converterManagedLabel)
}

func TestCreateGrafanaDashboardUpdatesMarkedCopy(t *testing.T) {
	existing := &v1beta1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-dashboard",
			Namespace: "product-a",
			Labels:    map[string]string{converterManagedLabel: converterManagedValue},
		},
		Spec: v1beta1.GrafanaDashboardSpec{Json: "old"},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
		Spec:       v1alpha1.GrafanaDashboardSpec{Json: "new"},
	}

	controller.createGrafanaDashboard(source)

	actual, err := client.GrafanaIntegreatlyV1beta1().GrafanaDashboards(existing.Namespace).Get(
		context.Background(), existing.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "new", actual.Spec.Json)
	assert.Equal(t, converterManagedValue, actual.Labels[converterManagedLabel])
}

func TestCreateGrafanaDashboardHandlesUpdateErrorWithoutPanic(t *testing.T) {
	existing := &v1beta1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-dashboard",
			Namespace: "product-a",
			Labels:    map[string]string{converterManagedLabel: converterManagedValue},
		},
		Spec: v1beta1.GrafanaDashboardSpec{Json: "old"},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	client.PrependReactor("update", "grafanadashboards", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("API update failed")
	})
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
		Spec:       v1alpha1.GrafanaDashboardSpec{Json: "new"},
	}

	assert.NotPanics(t, func() {
		controller.createGrafanaDashboard(source)
	})
}

func TestCreateGrafanaDatasourceDoesNotAdoptUnmarkedCollision(t *testing.T) {
	existing := &v1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{Name: "product-a-sample", Namespace: "product-a"},
		Spec: v1beta1.GrafanaDatasourceSpec{
			Datasource: &v1beta1.GrafanaDatasourceInternal{Name: "foreign"},
		},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaDataSource{
		ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: existing.Namespace},
		Spec: v1alpha1.GrafanaDataSourceSpec{
			Datasources: []v1alpha1.GrafanaDataSourceFields{{Name: "Sample", Type: "prometheus"}},
		},
	}

	controller.createGrafanaDatasource(source)

	actual, err := client.GrafanaIntegreatlyV1beta1().GrafanaDatasources(existing.Namespace).Get(
		context.Background(), existing.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.NotNil(t, actual.Spec.Datasource)
	assert.Equal(t, "foreign", actual.Spec.Datasource.Name)
}

func TestCreateGrafanaDatasourceHandlesUpdateErrorWithoutPanic(t *testing.T) {
	existing := &v1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "product-a-sample",
			Namespace: "product-a",
			Labels:    map[string]string{converterManagedLabel: converterManagedValue},
		},
		Spec: v1beta1.GrafanaDatasourceSpec{
			Datasource: &v1beta1.GrafanaDatasourceInternal{Name: "old"},
		},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	updateAttempted := false
	client.PrependReactor("update", "grafanadatasources", func(k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempted = true
		return true, nil, errors.New("API update failed")
	})
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaDataSource{
		ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: existing.Namespace},
		Spec: v1alpha1.GrafanaDataSourceSpec{
			Datasources: []v1alpha1.GrafanaDataSourceFields{{Name: "Sample", Type: "prometheus"}},
		},
	}

	assert.NotPanics(t, func() {
		controller.createGrafanaDatasource(source)
	})
	assert.True(t, updateAttempted)
}

func TestCreateGrafanaFolderDoesNotAdoptUnmarkedCollision(t *testing.T) {
	existing := &v1beta1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{Name: "sample-folder", Namespace: "product-a"},
		Spec:       v1beta1.GrafanaFolderSpec{Title: "foreign"},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
		Spec:       v1alpha1.GrafanaFolderSpec{FolderName: "converted"},
	}

	controller.createGrafanaFolder(source)

	actual, err := client.GrafanaIntegreatlyV1beta1().GrafanaFolders(existing.Namespace).Get(
		context.Background(), existing.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "foreign", actual.Spec.Title)
}

func TestCreateGrafanaFolderHandlesUpdateErrorWithoutPanic(t *testing.T) {
	existing := &v1beta1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-folder",
			Namespace: "product-a",
			Labels:    map[string]string{converterManagedLabel: converterManagedValue},
		},
		Spec: v1beta1.GrafanaFolderSpec{Title: "old"},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	updateAttempted := false
	client.PrependReactor("update", "grafanafolders", func(k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempted = true
		return true, nil, errors.New("API update failed")
	})
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
		Spec:       v1alpha1.GrafanaFolderSpec{FolderName: "new"},
	}

	assert.NotPanics(t, func() {
		controller.createGrafanaFolder(source)
	})
	assert.True(t, updateAttempted)
}

func TestCreateGrafanaNotificationChannelDoesNotAdoptUnmarkedCollision(t *testing.T) {
	existing := &v1beta1.GrafanaContactPoint{
		ObjectMeta: metav1.ObjectMeta{Name: "sample-contact-point", Namespace: "product-a"},
		Spec:       v1beta1.GrafanaContactPointSpec{Name: "foreign", Type: "email"},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"converted","type":"email","settings":{}}`,
		},
	}

	controller.createGrafanaNotificationChannel(source)

	actual, err := client.GrafanaIntegreatlyV1beta1().GrafanaContactPoints(existing.Namespace).Get(
		context.Background(), existing.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "foreign", actual.Spec.Name)
}

func TestCreateGrafanaNotificationChannelHandlesUpdateErrorWithoutPanic(t *testing.T) {
	existing := &v1beta1.GrafanaContactPoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-contact-point",
			Namespace: "product-a",
			Labels:    map[string]string{converterManagedLabel: converterManagedValue},
		},
		Spec: v1beta1.GrafanaContactPointSpec{Name: "old", Type: "email"},
	}
	client := v1beta1fake.NewSimpleClientset(existing)
	updateAttempted := false
	client.PrependReactor("update", "grafanacontactpoints", func(k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempted = true
		return true, nil, errors.New("API update failed")
	})
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"new","type":"email","settings":{}}`,
		},
	}

	assert.NotPanics(t, func() {
		controller.createGrafanaNotificationChannel(source)
	})
	assert.True(t, updateAttempted)
}

func TestCreateGrafanaNotificationChannelHandlesAlreadyExistsNotFoundRace(t *testing.T) {
	client := v1beta1fake.NewSimpleClientset()
	createAttempts := 0
	client.PrependReactor("create", "grafanacontactpoints", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAttempts++
		createAction := action.(k8stesting.CreateAction)
		return true, nil, apierrs.NewAlreadyExists(
			schema.GroupResource{Group: v1beta1.GroupVersion.Group, Resource: "grafanacontactpoints"},
			createAction.GetObject().(metav1.Object).GetName(),
		)
	})
	getAttempts := 0
	client.PrependReactor("get", "grafanacontactpoints", func(action k8stesting.Action) (bool, runtime.Object, error) {
		getAttempts++
		getAction := action.(k8stesting.GetAction)
		return true, nil, apierrs.NewNotFound(
			schema.GroupResource{Group: v1beta1.GroupVersion.Group, Resource: "grafanacontactpoints"},
			getAction.GetName(),
		)
	})
	controller := &ConverterController{log: logr.Discard(), v1beta1clientset: client}
	source := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "sample-contact-point", Namespace: "product-a"},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"sample","type":"email","settings":{}}`,
		},
	}

	assert.NotPanics(t, func() {
		controller.createGrafanaNotificationChannel(source)
	})
	assert.Equal(t, 2, createAttempts)
	assert.Equal(t, 1, getAttempts)
}
