package manager

import (
	"context"
	"flag"
	"os"
	"strings"

	v1alpha1clientset "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned"
	v1beta1clientset "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned"
	grafanav1alpha1 "github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	grafanav1beta1 "github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	converterController "github.com/Netcracker/qubership-grafana-operator-converter/controllers"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE which specifies the Namespace to watch.
	// If empty or undefined, the operator will run in cluster scope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"
	// watchNamespaceEnvSelector is the constant for env variable WATCH_NAMESPACE_SELECTOR which specifies the Namespace label and key to watch.
	// eg: "environment: dev"
	// If empty or undefined, the operator will run in cluster scope.
	watchNamespaceEnvSelector = "WATCH_NAMESPACE_SELECTOR"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	metricsAddr = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr   = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	pprofEnabled = flag.Bool("pprof-enable", false, "Enable pprof.")
	pprofAddr    = flag.String("pprof-address", ":9180", "The address the pprof endpoint binds to.")

	enableLeaderElection = flag.Bool("leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	kubeconfig *string
	
	resyncPeriod        = flag.Duration("controller.resyncPeriod", 0, "Configures resync period for grafana CRD converter. Disabled by default")
	converterConfigPath = flag.String("controller.config", "/opt/grafana-converter/parameters.yaml", "Grafana CRD converter configure.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// grafana v1alpha1 and v1beta1 APIs
	utilruntime.Must(grafanav1alpha1.AddToScheme(scheme))
	utilruntime.Must(grafanav1beta1.AddToScheme(scheme))

	// openshift route API
	utilruntime.Must(routev1.Install(scheme))

	// kubeconfig flag
	if existing := flag.Lookup("kubeconfig"); existing == nil {
		kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file (optional, if not set, in-cluster config is used)")
	} else {
		v := existing.Value.String()
		kubeconfig = &v
	}
}

func RunManager(ctx context.Context) (err error) {
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("Starting the Grafana Converter")

	// Get Kubernetes config
	var cfg *rest.Config
	if *kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		setupLog.Error(err, "unable to get kubernetes config")
		return err
	}

	watchNamespace, _ := os.LookupEnv(WatchNamespaceEnvVar)
	watchNamespaceSelector, _ := os.LookupEnv(watchNamespaceEnvSelector)

	if !*pprofEnabled {
		pprofAddr = nil
	}

	controllerOptions := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
		},
		ReadinessEndpointName: "/ready",
		LivenessEndpointName:  "/health",
		PprofBindAddress:      ptr.Deref(pprofAddr, ""),
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
		}),
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "f781j4kdn.monitoring.netcracker.com",
	}

	switch {
	case strings.Contains(watchNamespace, ","):
		// multi namespace scoped
		controllerOptions.Cache.DefaultNamespaces = getNamespaceConfig(watchNamespace)
		setupLog.Info("manager set up with multiple namespaces", "namespaces", watchNamespace)
	case watchNamespace != "":
		// namespace scoped
		controllerOptions.Cache.DefaultNamespaces = getNamespaceConfig(watchNamespace)
		setupLog.Info("converter running in namespace scoped mode", "namespace", watchNamespace)
	case strings.Contains(watchNamespaceSelector, ":"):
		// namespace scoped
		controllerOptions.Cache.DefaultNamespaces = getNamespaceConfigSelector(watchNamespaceSelector, cfg)
		setupLog.Info("converter running in namespace scoped mode using namespace selector", "namespace", watchNamespace)

	case watchNamespace == "" && watchNamespaceSelector == "":
		// cluster scoped
		setupLog.Info("converter running in cluster scoped mode")
	}

	var mgr manager.Manager
	mgr, err = ctrl.NewManager(cfg, controllerOptions)
	if err != nil {
		setupLog.Error(err, "unable to create new manager")
		return err
	}

	v1alpha1Client, err := v1alpha1clientset.NewForConfig(cfg)
	if err != nil {
		setupLog.Error(err, "Error building v1alpha1 clientset")
		return err
	}

	v1beta1Client, err := v1beta1clientset.NewForConfig(cfg)
	if err != nil {
		setupLog.Error(err, "Error building v1beta1 clientset")
		return err
	}

	converterController, err := converterController.NewGrafanaConverterController(ctx, *converterConfigPath, v1alpha1Client, v1beta1Client, *resyncPeriod, ctrl.Log.WithName("ConverterController"))
	if err != nil {
		setupLog.Error(err, "cannot setup grafana CRD converter")
	} else if converterController.ConverterConf.Enable {
		if err = mgr.Add(converterController); err != nil {
			setupLog.Error(err, "cannot add runnable")
			return err
		}
	}

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err = mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return err
}

func getNamespaceConfig(namespaces string) map[string]cache.Config {
	defaultNamespaces := map[string]cache.Config{}
	for _, v := range strings.Split(namespaces, ",") {
		// Generate a mapping of namespaces to label/field selectors, set to Everything() to enable matching all
		// instances in all namespaces from watchNamespace to be controlled by the operator
		// this is the default behavior of the operator on v5, if you require finer grained control over this
		// please file an issue in the grafana-operator/grafana-operator GH project
		defaultNamespaces[v] = cache.Config{
			LabelSelector:         labels.Everything(), // Match any labels
			FieldSelector:         fields.Everything(), // Match any fields
			Transform:             nil,
			UnsafeDisableDeepCopy: nil,
		}
	}
	return defaultNamespaces
}

func getNamespaceConfigSelector(selector string, cfg *rest.Config) map[string]cache.Config {
	var cl client.Client
	cl, err := client.New(cfg, client.Options{})
	if err != nil {
		setupLog.Error(err, "Failed to get watch namespaces")
	}
	nsList := &corev1.NamespaceList{}
	listOpts := []client.ListOption{
		client.MatchingLabels(map[string]string{strings.Split(selector, ":")[0]: strings.Split(selector, ":")[1]}),
	}
	err = cl.List(context.Background(), nsList, listOpts...)
	if err != nil {
		setupLog.Error(err, "Failed to get watch namespaces")
	}
	defaultNamespaces := map[string]cache.Config{}
	for _, v := range nsList.Items {
		// Generate a mapping of namespaces to label/field selectors, set to Everything() to enable matching all
		// instances in all namespaces from watchNamespace to be controlled by the operator
		// this is the default behavior of the operator on v5, if you require finer grained control over this
		// please file an issue in the grafana-operator/grafana-operator GH project
		defaultNamespaces[v.Name] = cache.Config{
			LabelSelector:         labels.Everything(), // Match any labels
			FieldSelector:         fields.Everything(), // Match any fields
			Transform:             nil,
			UnsafeDisableDeepCopy: nil,
		}
	}
	return defaultNamespaces
}
