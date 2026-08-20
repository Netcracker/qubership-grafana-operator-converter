package controllers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	v1alpha1clientset "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned"
	v1alpha1informers "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/informers/externalversions"
	v1beta1clientset "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned"
	v1beta1informers "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/informers/externalversions"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/yaml"
)

const (
	defaultCacheSyncTimeout        = 2 * time.Minute
	defaultDashboardAPITimeout     = 30 * time.Second
	dashboardRetryInitialBackoff   = 5 * time.Millisecond
	dashboardRetryMaximumBackoff   = time.Minute
	dashboardReconciliationWorkers = 1
)

// ConverterConfig defines converter configuration for Grafana v1alpha1 to v1beta1 api versions
type ConverterConfig struct {
	Enable                  bool                  `json:"enable,omitempty" yaml:"enable,omitempty"`
	Strategy                string                `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	InstanceSelector        *metav1.LabelSelector `json:"instanceSelector,omitempty" yaml:"instanceSelector,omitempty"`
	EnabledGrafanaConverter `json:",inline" yaml:",inline"`
}
type EnabledGrafanaConverter struct {
	Dashboard           bool `json:"dashboard,omitempty" yaml:"dashboard,omitempty"`
	Datasource          bool `json:"datasource,omitempty" yaml:"datasource,omitempty"`
	Folder              bool `json:"folder,omitempty" yaml:"folder,omitempty"`
	NotificationChannel bool `json:"notification,omitempty" yaml:"notification,omitempty"`
}

type informerFactory interface {
	Start(stopCh <-chan struct{})
	WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool
}

type scopedInformerFactory struct {
	scope   string
	factory informerFactory
}

type configuredInformerFactory struct {
	scope   string
	factory v1alpha1informers.SharedInformerFactory
}

type configuredV1beta1InformerFactory struct {
	scope   string
	factory v1beta1informers.SharedInformerFactory
}

// ConverterController watches legacy Grafana resources and reconciles their v1beta1 replacements.
type ConverterController struct {
	ctx                     context.Context
	log                     logr.Logger
	ConverterConf           ConverterConfig
	v1alpha1clientset       v1alpha1clientset.Interface
	v1beta1clientset        v1beta1clientset.Interface
	v1alpha1InformerFactory []scopedInformerFactory
	v1beta1InformerFactory  []scopedInformerFactory
	dashboardQueue          workqueue.TypedRateLimitingInterface[dashboardQueueItem]
	readinessMu             sync.RWMutex
	readinessErr            error
	cacheSyncTimeout        time.Duration
	apiTimeout              time.Duration
}

// NewGrafanaConverterController builder for grafana converter service
func NewGrafanaConverterController(ctx context.Context, converterConfigPath string, v1alpha1clientset v1alpha1clientset.Interface, v1beta1clientset v1beta1clientset.Interface, resyncPeriod time.Duration, log logr.Logger) (*ConverterController, error) {
	c := &ConverterController{
		ctx:               ctx,
		log:               log,
		ConverterConf:     ConverterConfig{},
		v1alpha1clientset: v1alpha1clientset,
		v1beta1clientset:  v1beta1clientset,
		cacheSyncTimeout:  defaultCacheSyncTimeout,
		apiTimeout:        defaultDashboardAPITimeout,
	}

	converterConfig, err := ReadConfig(converterConfigPath)
	if err != nil {
		log.Error(err, "can not read grafana converter configuration file, disabling grafana converter...")
		return c, err
	}

	log.Info(fmt.Sprintf("converter config: %+v\n", converterConfig))
	c.ConverterConf = *converterConfig
	if c.ConverterConf.Enable && c.ConverterConf.EnabledGrafanaConverter != (EnabledGrafanaConverter{}) {
		namespaces, namespaceErr := getWatchNamespaces()
		if namespaceErr != nil {
			return nil, fmt.Errorf("invalid watch namespace configuration: %w", namespaceErr)
		}
		configuredFactories := make([]configuredInformerFactory, 0, max(1, len(namespaces)))
		if len(namespaces) == 0 {
			configuredFactories = append(configuredFactories, configuredInformerFactory{
				scope:   "cluster-wide",
				factory: v1alpha1informers.NewSharedInformerFactory(v1alpha1clientset, resyncPeriod),
			})
		} else {
			for _, ns := range namespaces {
				configuredFactories = append(configuredFactories, configuredInformerFactory{
					scope:   ns,
					factory: v1alpha1informers.NewSharedInformerFactoryWithOptions(v1alpha1clientset, resyncPeriod, v1alpha1informers.WithNamespace(ns)),
				})
			}
		}

		if c.ConverterConf.Dashboard {
			c.dashboardQueue = newDashboardQueue()
			for _, scopedFactory := range configuredFactories {
				informer := scopedFactory.factory
				if _, err = informer.Integreatly().V1alpha1().GrafanaDashboards().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.enqueueSourceDashboard,
					UpdateFunc: func(_, newObject any) { c.enqueueSourceDashboard(newObject) },
					DeleteFunc: c.enqueueDeletedSourceDashboard,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana dashboards handler: %w", err)
				}
			}

			configuredV1beta1Factories := make([]configuredV1beta1InformerFactory, 0, len(configuredFactories))
			for _, configuredFactory := range configuredFactories {
				var factory v1beta1informers.SharedInformerFactory
				if configuredFactory.scope == "cluster-wide" {
					factory = v1beta1informers.NewSharedInformerFactory(v1beta1clientset, resyncPeriod)
				} else {
					factory = v1beta1informers.NewSharedInformerFactoryWithOptions(
						v1beta1clientset,
						resyncPeriod,
						v1beta1informers.WithNamespace(configuredFactory.scope),
					)
				}
				if _, err = factory.Observability().V1beta1().GrafanaDashboards().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.enqueueTargetDashboard,
					UpdateFunc: c.enqueueUpdatedTargetDashboard,
					DeleteFunc: c.enqueueDeletedTargetDashboard,
				}); err != nil {
					return nil, fmt.Errorf("cannot add target grafana dashboards handler: %w", err)
				}
				configuredV1beta1Factories = append(configuredV1beta1Factories, configuredV1beta1InformerFactory{
					scope:   configuredFactory.scope,
					factory: factory,
				})
			}
			for _, configuredFactory := range configuredV1beta1Factories {
				c.v1beta1InformerFactory = append(c.v1beta1InformerFactory, scopedInformerFactory{
					scope:   configuredFactory.scope,
					factory: configuredFactory.factory,
				})
			}
		}

		if c.ConverterConf.Datasource {
			for _, scopedFactory := range configuredFactories {
				informer := scopedFactory.factory
				if _, err = informer.Integreatly().V1alpha1().GrafanaDataSources().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.createGrafanaDatasource,
					UpdateFunc: c.updateGrafanaDatasource,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana datasource handler: %w", err)
				}
			}
		}

		if c.ConverterConf.Folder {
			for _, scopedFactory := range configuredFactories {
				informer := scopedFactory.factory
				if _, err = informer.Integreatly().V1alpha1().GrafanaFolders().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.createGrafanaFolder,
					UpdateFunc: c.updateGrafanaFolder,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana folder handler: %w", err)
				}
			}
		}

		if c.ConverterConf.NotificationChannel {
			for _, scopedFactory := range configuredFactories {
				informer := scopedFactory.factory
				if _, err = informer.Integreatly().V1alpha1().GrafanaNotificationChannels().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.createGrafanaNotificationChannel,
					UpdateFunc: c.updateGrafanaNotificationChannel,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana notification channel handler: %w", err)
				}
			}
		}

		for _, configuredFactory := range configuredFactories {
			c.v1alpha1InformerFactory = append(c.v1alpha1InformerFactory, scopedInformerFactory{
				scope:   configuredFactory.scope,
				factory: configuredFactory.factory,
			})
		}
		c.setReadiness(fmt.Errorf("waiting for informer caches to synchronize: %s", strings.Join(c.informerScopes(), ", ")))
	}

	return c, nil
}

// Start implements interface.
// nolint:unparam
func (c *ConverterController) Start(ctx context.Context) error {
	c.log.Info("starting grafana converter")

	for _, scopedFactory := range c.informerFactories() {
		scopedFactory.factory.Start(ctx.Done())
	}
	syncCtx, cancelSync := context.WithTimeout(ctx, c.cacheSyncTimeout)
	defer cancelSync()
	for _, scopedFactory := range c.informerFactories() {
		failedTypes := failedCacheTypes(scopedFactory.factory.WaitForCacheSync(syncCtx.Done()))
		if len(failedTypes) > 0 {
			if ctx.Err() != nil {
				return nil
			}
			err := fmt.Errorf("failed to synchronize informer caches for %s: %s", scopedFactory.scope, strings.Join(failedTypes, ", "))
			c.setReadiness(err)
			return err
		}
	}

	c.setReadiness(nil)
	c.log.Info("grafana converter started")

	var workers sync.WaitGroup
	if c.dashboardQueue != nil {
		for range dashboardReconciliationWorkers {
			workers.Add(1)
			go func() {
				defer workers.Done()
				c.runDashboardWorker(ctx)
			}()
		}
	}
	<-ctx.Done()
	if c.dashboardQueue != nil {
		c.dashboardQueue.ShutDown()
		workers.Wait()
	}
	return nil
}

// ReadinessCheck reports whether every informer cache required by the enabled converters has synchronized.
func (c *ConverterController) ReadinessCheck(_ *http.Request) error {
	c.readinessMu.RLock()
	defer c.readinessMu.RUnlock()
	return c.readinessErr
}

func (c *ConverterController) setReadiness(err error) {
	c.readinessMu.Lock()
	defer c.readinessMu.Unlock()
	c.readinessErr = err
}

func (c *ConverterController) informerScopes() []string {
	uniqueScopes := make(map[string]struct{})
	for _, scopedFactory := range c.informerFactories() {
		uniqueScopes[scopedFactory.scope] = struct{}{}
	}
	scopes := make([]string, 0, len(uniqueScopes))
	for scope := range uniqueScopes {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func (c *ConverterController) informerFactories() []scopedInformerFactory {
	factories := make([]scopedInformerFactory, 0, len(c.v1alpha1InformerFactory)+len(c.v1beta1InformerFactory))
	factories = append(factories, c.v1alpha1InformerFactory...)
	factories = append(factories, c.v1beta1InformerFactory...)
	return factories
}

func newDashboardQueue() workqueue.TypedRateLimitingInterface[dashboardQueueItem] {
	return workqueue.NewTypedRateLimitingQueueWithConfig(newDashboardRateLimiter(), workqueue.TypedRateLimitingQueueConfig[dashboardQueueItem]{
		Name: "grafana-dashboard-conversion",
	})
}

func newDashboardRateLimiter() workqueue.TypedRateLimiter[dashboardQueueItem] {
	return workqueue.NewTypedItemExponentialFailureRateLimiter[dashboardQueueItem](
		dashboardRetryInitialBackoff,
		dashboardRetryMaximumBackoff,
	)
}

func failedCacheTypes(syncResults map[reflect.Type]bool) []string {
	failedTypes := make([]string, 0)
	for informerType, synced := range syncResults {
		if !synced {
			failedTypes = append(failedTypes, informerType.String())
		}
	}
	sort.Strings(failedTypes)
	return failedTypes
}

func ReadConfig(path string) (*ConverterConfig, error) {
	converterConfig := &ConverterConfig{}

	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConverterConfig{}, nil
		}
		return &ConverterConfig{}, err
	}
	if err = yaml.UnmarshalStrict(contents, converterConfig); err != nil {
		return &ConverterConfig{}, fmt.Errorf("decode converter config %q: %w", path, err)
	}
	if converterConfig.Enable && converterConfig.EnabledGrafanaConverter == (EnabledGrafanaConverter{}) {
		return &ConverterConfig{}, fmt.Errorf("at least one converter must be enabled when conversion is enabled")
	}
	if converterConfig.InstanceSelector != nil {
		if _, selectorErr := metav1.LabelSelectorAsSelector(converterConfig.InstanceSelector); selectorErr != nil {
			return &ConverterConfig{}, fmt.Errorf("invalid instanceSelector: %w", selectorErr)
		}
	}
	return converterConfig, nil
}

// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
// which specifies the Namespace to watch.
// An empty value means the operator is running with cluster scope.
const WatchNamespaceEnvVar = "WATCH_NAMESPACE"

func getWatchNamespaces() ([]string, error) {
	wns, _ := os.LookupEnv(WatchNamespaceEnvVar)
	if len(wns) > 0 {
		nss := strings.Split(wns, ",")
		for _, ns := range nss {
			if validationErrors := validation.IsDNS1123Label(ns); len(validationErrors) > 0 {
				return nil, fmt.Errorf("invalid namespace %q in %s=%q: %s", ns, WatchNamespaceEnvVar, wns, strings.Join(validationErrors, "; "))
			}
		}

		return nss, nil
	}
	return nil, nil
}
