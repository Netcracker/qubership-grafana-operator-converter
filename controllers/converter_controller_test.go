package controllers

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	v1alpha1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned/fake"
	v1beta1fake "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned/fake"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
)

func TestReadConfig(t *testing.T) {
	t.Run("missing file returns empty config", func(t *testing.T) {
		cfg, err := ReadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
		require.NoError(t, err)
		assert.Equal(t, &ConverterConfig{}, cfg)
	})

	t.Run("yaml file is parsed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		content := []byte("enable: true\nstrategy: sync\ndashboard: true\ndatasource: true\nfolder: false\nnotification: true\ninstanceSelector:\n  matchLabels:\n    app: grafana\n")
		require.NoError(t, os.WriteFile(path, content, 0o600))

		cfg, err := ReadConfig(path)
		require.NoError(t, err)
		assert.True(t, cfg.Enable)
		assert.Equal(t, "sync", cfg.Strategy)
		assert.True(t, cfg.Dashboard)
		assert.True(t, cfg.Datasource)
		assert.False(t, cfg.Folder)
		assert.True(t, cfg.NotificationChannel)
		require.NotNil(t, cfg.InstanceSelector)
		assert.Equal(t, map[string]string{"app": "grafana"}, cfg.InstanceSelector.MatchLabels)
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(":\n"), 0o600))

		_, err := ReadConfig(path)
		require.Error(t, err)
	})
}

func TestGetWatchNamespaces(t *testing.T) {
	t.Run("env is empty", func(t *testing.T) {
		t.Setenv(WatchNamespaceEnvVar, "")
		namespaces, err := getWatchNamespaces()
		require.NoError(t, err)
		assert.Nil(t, namespaces)
	})

	t.Run("valid namespaces are returned", func(t *testing.T) {
		t.Setenv(WatchNamespaceEnvVar, "team-a,team-b")
		namespaces, err := getWatchNamespaces()
		require.NoError(t, err)
		assert.Equal(t, []string{"team-a", "team-b"}, namespaces)
	})

	t.Run("invalid namespace returns error", func(t *testing.T) {
		t.Setenv(WatchNamespaceEnvVar, "team-a,")
		_, err := getWatchNamespaces()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incorrect namespace")
	})
}

func TestMustGetWatchNamespacesCachesValue(t *testing.T) {
	initNamespace = sync.Once{}
	opNamespace = nil
	t.Setenv(WatchNamespaceEnvVar, "team-a")
	assert.Equal(t, []string{"team-a"}, mustGetWatchNamespaces())

	t.Setenv(WatchNamespaceEnvVar, "team-b")
	assert.Equal(t, []string{"team-a"}, mustGetWatchNamespaces())

	initNamespace = sync.Once{}
	opNamespace = nil
}

func TestBuildFolderPermission(t *testing.T) {
	permissions := []*v1alpha1.GrafanaPermissionItem{
		{PermissionTargetType: "teamId", PermissionTarget: "15", PermissionLevel: 2},
		{PermissionTargetType: "role", PermissionTarget: "Admin", PermissionLevel: 4},
	}

	assert.Equal(t, `{ "items": [ {"teamId": 15, "permission": 2},{"role": "Admin", "permission": 4} ]}`, buildFolderPermission(permissions))
}

func TestJSONPtr(t *testing.T) {
	value := map[string]interface{}{"name": "alerts", "count": 2}
	got := jsonPtr(value)
	require.NotNil(t, got)
	assert.JSONEq(t, `{"count":2,"name":"alerts"}`, string(got.Raw))
}

func TestConvertGrafanaDashboard(t *testing.T) {
	controller := newControllerForTests()
	revision := 7
	cacheDuration := metav1.Duration{Duration: 3 * time.Minute}
	src := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dash",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaDashboardSpec{
			Json:             `{"title":"A"}`,
			GzipJson:         []byte("gzip"),
			Url:              "https://example.test/dashboard.json",
			Jsonnet:          "main.jsonnet",
			ConfigMapRef:     nil,
			CustomFolderName: "Operations",
			Plugins: []v1alpha1.GrafanaPlugin{
				{Name: "clock-panel", Version: "1.0.0"},
			},
			Datasources: []v1alpha1.GrafanaDashboardDatasource{
				{InputName: "DS_PROM", DatasourceName: "prometheus"},
			},
			GrafanaCom: &v1alpha1.GrafanaDashboardGrafanaComSource{
				Id:       42,
				Revision: &revision,
			},
			ContentCacheDuration: &cacheDuration,
		},
	}

	converted := controller.convertGrafanaDashboard(src)
	require.NotNil(t, converted)
	assert.Equal(t, src.Name, converted.Name)
	assert.Equal(t, src.Namespace, converted.Namespace)
	assert.Equal(t, src.Labels, converted.Labels)
	assert.Equal(t, src.Annotations, converted.Annotations)
	assert.Equal(t, src.Spec.Json, converted.Spec.Json)
	assert.Equal(t, src.Spec.GzipJson, converted.Spec.GzipJson)
	assert.Equal(t, src.Spec.Url, converted.Spec.Url)
	assert.Equal(t, src.Spec.Jsonnet, converted.Spec.Jsonnet)
	assert.Equal(t, src.Spec.CustomFolderName, converted.Spec.FolderTitle)
	assert.Equal(t, src.Spec.Datasources[0].InputName, converted.Spec.Datasources[0].InputName)
	assert.Equal(t, src.Spec.Plugins[0].Name, converted.Spec.Plugins[0].Name)
	assert.Equal(t, controller.ConverterConf.InstanceSelector, converted.Spec.InstanceSelector)
	assert.Equal(t, v1beta1.DefaultResyncPeriod, converted.Spec.ResyncPeriod)
	assert.Equal(t, cacheDuration.Duration, converted.Spec.ContentCacheDuration.Duration)
	require.NotNil(t, converted.Spec.AllowCrossNamespaceImport)
	assert.True(t, *converted.Spec.AllowCrossNamespaceImport)
	require.NotNil(t, converted.Spec.GrafanaCom)
	assert.Equal(t, 42, converted.Spec.GrafanaCom.Id)
	require.NotNil(t, converted.Spec.GrafanaCom.Revision)
	assert.Equal(t, revision, *converted.Spec.GrafanaCom.Revision)
}

func TestNewGrafanaConverterController(t *testing.T) {
	t.Run("missing config keeps converter disabled", func(t *testing.T) {
		initNamespace = sync.Once{}
		opNamespace = nil
		controller, err := NewGrafanaConverterController(
			context.Background(),
			filepath.Join(t.TempDir(), "missing.yaml"),
			v1alpha1fake.NewSimpleClientset(),
			v1beta1fake.NewSimpleClientset(),
			time.Second,
			logr.Discard(),
		)
		require.NoError(t, err)
		assert.False(t, controller.ConverterConf.Enable)
		assert.Empty(t, controller.v1alpha1InformerFactory)
	})

	t.Run("enabled config creates informer factories and start succeeds", func(t *testing.T) {
		initNamespace = sync.Once{}
		opNamespace = nil
		t.Setenv(WatchNamespaceEnvVar, "team-a,team-b")
		path := filepath.Join(t.TempDir(), "config.yaml")
		content := []byte("enable: true\ndashboard: true\ndatasource: true\nfolder: true\nnotification: true\n")
		require.NoError(t, os.WriteFile(path, content, 0o600))

		controller, err := NewGrafanaConverterController(
			context.Background(),
			path,
			v1alpha1fake.NewSimpleClientset(),
			v1beta1fake.NewSimpleClientset(),
			time.Second,
			logr.Discard(),
		)
		require.NoError(t, err)
		assert.True(t, controller.ConverterConf.Enable)
		assert.Len(t, controller.v1alpha1InformerFactory, 2)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.NoError(t, controller.Start(ctx))

		initNamespace = sync.Once{}
		opNamespace = nil
	})

	t.Run("invalid config returns error", func(t *testing.T) {
		initNamespace = sync.Once{}
		opNamespace = nil
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(":\n"), 0o600))

		controller, err := NewGrafanaConverterController(
			context.Background(),
			path,
			v1alpha1fake.NewSimpleClientset(),
			v1beta1fake.NewSimpleClientset(),
			time.Second,
			logr.Discard(),
		)
		require.Error(t, err)
		require.NotNil(t, controller)
		assert.False(t, controller.ConverterConf.Enable)
	})
}

func TestCreateAndUpdateGrafanaDashboard(t *testing.T) {
	controller := newControllerForTests()
	alpha := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dash",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaDashboardSpec{
			Json:             `{"title":"A"}`,
			CustomFolderName: "Folder A",
		},
	}

	controller.createGrafanaDashboard(alpha)

	created, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDashboards("monitoring").Get(context.Background(), "dash", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Folder A", created.Spec.FolderTitle)

	updatedAlpha := alpha.DeepCopy()
	updatedAlpha.Spec.CustomFolderName = "Folder B"
	updatedAlpha.Spec.Json = `{"title":"B"}`
	updatedAlpha.Annotations = map[string]string{"source": "updated"}
	updatedAlpha.Labels = map[string]string{"app": "grafana", "tier": "ops"}
	controller.updateGrafanaDashboard(alpha, updatedAlpha)

	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDashboards("monitoring").Get(context.Background(), "dash", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Folder B", updated.Spec.FolderTitle)
	assert.Equal(t, `{"title":"B"}`, updated.Spec.Json)
	assert.Equal(t, "updated", updated.Annotations["source"])
	assert.Equal(t, "ops", updated.Labels["tier"])
}

func TestDashboardCreateAlreadyExistsAndUpdateCreatesWhenMissing(t *testing.T) {
	alpha := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dash",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaDashboardSpec{
			Json:             `{"title":"new"}`,
			CustomFolderName: "Folder New",
		},
	}

	controller := newControllerForTests(&v1beta1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dash",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "old"},
		},
		Spec: v1beta1.GrafanaDashboardSpec{
			Json:        `{"title":"old"}`,
			FolderTitle: "Folder Old",
		},
	})

	controller.createGrafanaDashboard(alpha)
	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDashboards("monitoring").Get(context.Background(), "dash", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Folder New", updated.Spec.FolderTitle)
	assert.Equal(t, `{"title":"new"}`, updated.Spec.Json)

	missingController := newControllerForTests()
	beta := controller.convertGrafanaDashboard(alpha)
	missingController.updateGrafanaDashboard(nil, beta)
	created, err := missingController.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDashboards("monitoring").Get(context.Background(), "dash", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Folder New", created.Spec.FolderTitle)
}

func TestUpdateGrafanaDashboardNoDiffSkipsUpdate(t *testing.T) {
	controller := newControllerForTests()
	alpha := &v1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{Name: "dash", Namespace: "monitoring"},
		Spec:       v1alpha1.GrafanaDashboardSpec{Json: `{"title":"same"}`},
	}

	controller.updateGrafanaDashboard(alpha, alpha.DeepCopy())

	_, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDashboards("monitoring").Get(context.Background(), "dash", metav1.GetOptions{})
	require.Error(t, err)
}

func TestConvertGrafanaDatasource(t *testing.T) {
	controller := newControllerForTests()
	src := &v1alpha1.GrafanaDataSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "datasources",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaDataSourceSpec{
			Datasources: []v1alpha1.GrafanaDataSourceFields{
				{
					Name:           "Prometheus Main",
					Type:           "prometheus",
					Uid:            "custom-uid",
					Url:            "http://prometheus",
					IsDefault:      true,
					BasicAuth:      true,
					BasicAuthUser:  "admin",
					Editable:       true,
					OrgId:          2,
					JsonData:       v1alpha1.GrafanaDataSourceJsonData{HTTPMethod: "POST"},
					SecureJsonData: v1alpha1.GrafanaDataSourceSecureJsonData{Password: "secret"},
				},
				{
					Name:                 "Loki Secondary",
					Type:                 "loki",
					CustomJsonData:       []byte(`{"timeout":30}`),
					CustomSecureJsonData: []byte(`{"token":"secret"}`),
				},
			},
		},
	}

	converted, err := controller.convertGrafanaDatasource(src)
	require.NoError(t, err)
	require.Len(t, converted, 2)

	assert.Equal(t, "monitoring-prometheus-main", converted[0].Name)
	assert.Equal(t, oldDatasourceUID, converted[0].Spec.Datasource.UID)
	assert.JSONEq(t, `{"password":"secret"}`, string(converted[0].Spec.Datasource.SecureJSONData))
	require.NotNil(t, converted[0].Spec.AllowCrossNamespaceImport)
	assert.True(t, *converted[0].Spec.AllowCrossNamespaceImport)
	assert.Equal(t, controller.ConverterConf.InstanceSelector, converted[0].Spec.InstanceSelector)
	var jsonData map[string]interface{}
	require.NoError(t, json.Unmarshal(converted[0].Spec.Datasource.JSONData, &jsonData))
	assert.Equal(t, "POST", jsonData["httpMethod"])

	assert.Equal(t, "monitoring-loki-secondary", converted[1].Name)
	assert.Equal(t, "loki", converted[1].Spec.Datasource.Type)
	assert.JSONEq(t, `{"timeout":30}`, string(converted[1].Spec.Datasource.JSONData))
	assert.JSONEq(t, `{"token":"secret"}`, string(converted[1].Spec.Datasource.SecureJSONData))
}

func TestCreateAndUpdateGrafanaDatasource(t *testing.T) {
	controller := newControllerForTests()
	alpha := &v1alpha1.GrafanaDataSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "datasources",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaDataSourceSpec{
			Datasources: []v1alpha1.GrafanaDataSourceFields{
				{Name: "Loki Main", Type: "loki", Url: "http://loki"},
			},
		},
	}

	controller.createGrafanaDatasource(alpha)

	created, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDatasources("monitoring").Get(context.Background(), "monitoring-loki-main", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "http://loki", created.Spec.Datasource.URL)

	updatedAlpha := alpha.DeepCopy()
	updatedAlpha.Spec.Datasources[0].Url = "http://loki-v2"
	updatedAlpha.Annotations = map[string]string{"source": "updated"}
	updatedAlpha.Labels = map[string]string{"app": "grafana", "tier": "ops"}
	controller.updateGrafanaDatasource(alpha, updatedAlpha)

	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDatasources("monitoring").Get(context.Background(), "monitoring-loki-main", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "http://loki-v2", updated.Spec.Datasource.URL)
	assert.Equal(t, "updated", updated.Annotations["source"])
	assert.Equal(t, "ops", updated.Labels["tier"])
}

func TestDatasourceCreateAlreadyExistsAndUpdateCreatesWhenMissing(t *testing.T) {
	alpha := &v1alpha1.GrafanaDataSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "datasources",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaDataSourceSpec{
			Datasources: []v1alpha1.GrafanaDataSourceFields{
				{Name: "Loki Main", Type: "loki", Url: "http://loki-v2"},
			},
		},
	}

	controller := newControllerForTests(&v1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "monitoring-loki-main",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "old"},
		},
		Spec: v1beta1.GrafanaDatasourceSpec{
			Datasource: &v1beta1.GrafanaDatasourceInternal{Name: "Loki Main", URL: "http://loki-old"},
		},
	})

	controller.createGrafanaDatasource(alpha)
	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDatasources("monitoring").Get(context.Background(), "monitoring-loki-main", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "http://loki-v2", updated.Spec.Datasource.URL)

	missingController := newControllerForTests()
	betaResources, err := controller.convertGrafanaDatasource(alpha)
	require.NoError(t, err)
	missingController.updateGrafanaDatasource(nil, betaResources[0])
	created, err := missingController.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDatasources("monitoring").Get(context.Background(), "monitoring-loki-main", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "http://loki-v2", created.Spec.Datasource.URL)
}

func TestUpdateGrafanaDatasourceNoDiffSkipsUpdate(t *testing.T) {
	controller := newControllerForTests()
	alpha := &v1alpha1.GrafanaDataSource{
		ObjectMeta: metav1.ObjectMeta{Name: "datasources", Namespace: "monitoring"},
		Spec: v1alpha1.GrafanaDataSourceSpec{
			Datasources: []v1alpha1.GrafanaDataSourceFields{
				{Name: "Loki Main", Type: "loki", Url: "http://loki"},
			},
		},
	}

	controller.updateGrafanaDatasource(alpha, alpha.DeepCopy())

	_, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDatasources("monitoring").Get(context.Background(), "monitoring-loki-main", metav1.GetOptions{})
	require.Error(t, err)
}

func TestConvertGrafanaFolderAndLifecycle(t *testing.T) {
	controller := newControllerForTests()
	alpha := &v1alpha1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "folder",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaFolderSpec{
			FolderName: "Operations",
			FolderPermissions: []v1alpha1.GrafanaPermissionItem{
				{PermissionTargetType: "teamId", PermissionTarget: "7", PermissionLevel: 1},
			},
		},
	}

	converted := controller.convertGrafanaFolder(alpha)
	require.NotNil(t, converted)
	assert.Equal(t, "Operations", converted.Spec.Title)
	assert.Equal(t, `{ "items": [ {"teamId": 7, "permission": 1} ]}`, converted.Spec.Permissions)
	require.NotNil(t, converted.Spec.AllowCrossNamespaceImport)
	assert.True(t, *converted.Spec.AllowCrossNamespaceImport)

	controller.createGrafanaFolder(alpha)
	created, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders("monitoring").Get(context.Background(), "folder", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Operations", created.Spec.Title)

	updatedAlpha := alpha.DeepCopy()
	updatedAlpha.Spec.FolderName = "SRE"
	updatedAlpha.Annotations = map[string]string{"source": "updated"}
	updatedAlpha.Labels = map[string]string{"app": "grafana", "tier": "ops"}
	controller.updateGrafanaFolder(alpha, updatedAlpha)

	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders("monitoring").Get(context.Background(), "folder", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "SRE", updated.Spec.Title)
	assert.Equal(t, "updated", updated.Annotations["source"])
	assert.Equal(t, "ops", updated.Labels["tier"])
}

func TestFolderCreateAlreadyExistsAndUpdateCreatesWhenMissing(t *testing.T) {
	alpha := &v1alpha1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "folder",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaFolderSpec{
			FolderName: "SRE",
		},
	}

	controller := newControllerForTests(&v1beta1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "folder",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "old"},
		},
		Spec: v1beta1.GrafanaFolderSpec{
			Title: "Old",
		},
	})

	controller.createGrafanaFolder(alpha)
	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders("monitoring").Get(context.Background(), "folder", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "SRE", updated.Spec.Title)

	missingController := newControllerForTests()
	beta := controller.convertGrafanaFolder(alpha)
	missingController.updateGrafanaFolder(nil, beta)
	created, err := missingController.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders("monitoring").Get(context.Background(), "folder", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "SRE", created.Spec.Title)
}

func TestUpdateGrafanaFolderNoDiffSkipsUpdate(t *testing.T) {
	controller := newControllerForTests()
	alpha := &v1alpha1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{Name: "folder", Namespace: "monitoring"},
		Spec:       v1alpha1.GrafanaFolderSpec{FolderName: "same"},
	}

	controller.updateGrafanaFolder(alpha, alpha.DeepCopy())

	_, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders("monitoring").Get(context.Background(), "folder", metav1.GetOptions{})
	require.Error(t, err)
}

func TestConvertGrafanaNotificationChannelAndLifecycle(t *testing.T) {
	controller := newControllerForTests()
	alpha := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "alerts",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"Alerts","type":"email","disableResolveMessage":true,"settings":{"addresses":"dev@example.com"}}`,
		},
	}

	converted, err := controller.convertGrafanaNotificationChannel(alpha)
	require.NoError(t, err)
	require.NotNil(t, converted)
	assert.Equal(t, "Alerts", converted.Spec.Name)
	assert.Equal(t, "email", converted.Spec.Type)
	assert.True(t, converted.Spec.DisableResolveMessage)
	assert.JSONEq(t, `{"addresses":"dev@example.com"}`, string(converted.Spec.Settings.Raw))
	require.NotNil(t, converted.Spec.AllowCrossNamespaceImport)
	assert.True(t, *converted.Spec.AllowCrossNamespaceImport)

	controller.createGrafanaNotificationChannel(alpha)
	created, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaContactPoints("monitoring").Get(context.Background(), "alerts", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Alerts", created.Spec.Name)

	updatedAlpha := alpha.DeepCopy()
	updatedAlpha.Spec.Json = `{"name":"Alerts v2","type":"email","disableResolveMessage":false,"settings":{"addresses":"sre@example.com"}}`
	updatedAlpha.Annotations = map[string]string{"source": "updated"}
	updatedAlpha.Labels = map[string]string{"app": "grafana", "tier": "ops"}
	controller.updateGrafanaNotificationChannel(alpha, updatedAlpha)

	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaContactPoints("monitoring").Get(context.Background(), "alerts", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Alerts v2", updated.Spec.Name)
	assert.False(t, updated.Spec.DisableResolveMessage)
	assert.JSONEq(t, `{"addresses":"sre@example.com"}`, string(updated.Spec.Settings.Raw))
	assert.Equal(t, "updated", updated.Annotations["source"])
	assert.Equal(t, "ops", updated.Labels["tier"])
}

func TestConvertGrafanaNotificationChannelInvalidJSON(t *testing.T) {
	controller := newControllerForTests()
	_, err := controller.convertGrafanaNotificationChannel(&v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "alerts", Namespace: "monitoring"},
		Spec:       v1alpha1.GrafanaNotificationChannelSpec{Json: `{`},
	})
	require.Error(t, err)
}

func newControllerForTests(objects ...runtime.Object) *ConverterController {
	return &ConverterController{
		ctx:              context.Background(),
		log:              logr.Discard(),
		v1beta1clientset: v1beta1fake.NewSimpleClientset(objects...),
		ConverterConf: ConverterConfig{
			InstanceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"grafana": "main"},
			},
		},
	}
}

func TestCreateGrafanaNotificationChannelAlreadyExistsUpdates(t *testing.T) {
	controller := &ConverterController{
		ctx: context.Background(),
		log: logr.Discard(),
		v1beta1clientset: v1beta1fake.NewSimpleClientset(&v1beta1.GrafanaContactPoint{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "alerts",
				Namespace:   "monitoring",
				Labels:      map[string]string{"app": "grafana"},
				Annotations: map[string]string{"source": "existing"},
			},
			Spec: v1beta1.GrafanaContactPointSpec{
				Name:     "Old",
				Type:     "email",
				Settings: jsonPtr(map[string]string{"addresses": "old@example.com"}),
			},
		}),
		ConverterConf: ConverterConfig{
			InstanceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"grafana": "main"},
			},
		},
	}

	alpha := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "alerts",
			Namespace:   "monitoring",
			Labels:      map[string]string{"app": "grafana"},
			Annotations: map[string]string{"source": "alpha"},
		},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"New","type":"email","settings":{"addresses":"new@example.com"}}`,
		},
	}

	controller.createGrafanaNotificationChannel(alpha)

	updated, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaContactPoints("monitoring").Get(context.Background(), "alerts", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "New", updated.Spec.Name)
	assert.JSONEq(t, `{"addresses":"new@example.com"}`, string(updated.Spec.Settings.Raw))
}

func TestUpdateGrafanaNotificationChannelCreatesWhenMissingAndSkipsNoDiff(t *testing.T) {
	controller := newControllerForTests()
	oldAlpha := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "alerts", Namespace: "monitoring"},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"Old","type":"email","settings":{"addresses":"old@example.com"}}`,
		},
	}
	newAlpha := oldAlpha.DeepCopy()
	newAlpha.Spec.Json = `{"name":"New","type":"email","settings":{"addresses":"new@example.com"}}`

	controller.updateGrafanaNotificationChannel(oldAlpha, newAlpha)

	created, err := controller.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaContactPoints("monitoring").Get(context.Background(), "alerts", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "New", created.Spec.Name)

	noDiffController := newControllerForTests()
	noDiff := &v1alpha1.GrafanaNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: "monitoring"},
		Spec: v1alpha1.GrafanaNotificationChannelSpec{
			Json: `{"name":"Same","type":"email","settings":{"addresses":"same@example.com"}}`,
		},
	}
	noDiffController.updateGrafanaNotificationChannel(noDiff, noDiff.DeepCopy())

	_, err = noDiffController.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaContactPoints("monitoring").Get(context.Background(), "same", metav1.GetOptions{})
	require.Error(t, err)
}
