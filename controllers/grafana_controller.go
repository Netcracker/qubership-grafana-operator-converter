package controllers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	v1alpha1clientset "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/clientset/versioned"
	v1alpha1informers "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1alpha1/informers/externalversions"
	v1beta1clientset "github.com/Netcracker/qubership-grafana-operator-converter/api/client/v1beta1/clientset/versioned"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/cache"
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

// ConverterController - watches for grafana integreatly.org/v1alpha1 objects
// and create\update grafana.integreatly.org/v1beta1 objects
type ConverterController struct {
	ctx                     context.Context
	log                     logr.Logger
	ConverterConf           ConverterConfig
	v1beta1clientset        v1beta1clientset.Interface
	v1alpha1InformerFactory []v1alpha1informers.SharedInformerFactory
}

// NewGrafanaConverterController builder for grafana converter service
func NewGrafanaConverterController(ctx context.Context, converterConfigPath string, v1alpha1clientset v1alpha1clientset.Interface, v1beta1clientset v1beta1clientset.Interface, resyncPeriod time.Duration, log logr.Logger) (*ConverterController, error) {
	c := &ConverterController{
		ctx:              ctx,
		log:              log,
		ConverterConf:    ConverterConfig{},
		v1beta1clientset: v1beta1clientset,
	}

	converterConfig, err := ReadConfig(converterConfigPath)
	if err != nil {
		log.Error(err, "can not read grafana converter configuration file, disabling grafana converter...")
		return c, err
	}

	log.Info(fmt.Sprintf("converter config: %+v\n", converterConfig))
	c.ConverterConf = *converterConfig
	if c.ConverterConf.Enable && c.ConverterConf.EnabledGrafanaConverter != (EnabledGrafanaConverter{}) {
		namespaces := mustGetWatchNamespaces()
		if len(namespaces) == 0 {
			c.v1alpha1InformerFactory = append(c.v1alpha1InformerFactory, v1alpha1informers.NewSharedInformerFactory(v1alpha1clientset, resyncPeriod))
		} else {
			for _, ns := range namespaces {
				c.v1alpha1InformerFactory = append(c.v1alpha1InformerFactory, v1alpha1informers.NewSharedInformerFactoryWithOptions(v1alpha1clientset, resyncPeriod, v1alpha1informers.WithNamespace(ns)))
			}
		}

		if c.ConverterConf.Dashboard {
			for _, informer := range c.v1alpha1InformerFactory {
				if _, err = informer.Integreatly().V1alpha1().GrafanaDashboards().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.createGrafanaDashboard,
					UpdateFunc: c.updateGrafanaDashboard,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana dashboards handler: %w", err)
				}
			}
		}

		if c.ConverterConf.Datasource {
			for _, informer := range c.v1alpha1InformerFactory {
				if _, err = informer.Integreatly().V1alpha1().GrafanaDataSources().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.createGrafanaDatasource,
					UpdateFunc: c.updateGrafanaDatasource,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana datasource handler: %w", err)
				}
			}
		}

		if c.ConverterConf.Folder {
			for _, informer := range c.v1alpha1InformerFactory {
				if _, err = informer.Integreatly().V1alpha1().GrafanaFolders().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.createGrafanaFolder,
					UpdateFunc: c.updateGrafanaFolder,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana folder handler: %w", err)
				}
			}
		}

		if c.ConverterConf.NotificationChannel {
			for _, informer := range c.v1alpha1InformerFactory {
				if _, err = informer.Integreatly().V1alpha1().GrafanaNotificationChannels().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc:    c.createGrafanaNotificationChannel,
					UpdateFunc: c.updateGrafanaNotificationChannel,
				}); err != nil {
					return nil, fmt.Errorf("cannot add grafana notification channel handler: %w", err)
				}
			}
		}
	}

	return c, nil
}

// Start implements interface.
// nolint:unparam
func (c *ConverterController) Start(ctx context.Context) error {
	c.log.Info("starting grafana converter")

	for _, informerFactory := range c.v1alpha1InformerFactory {
		informerFactory.Start(ctx.Done())
		informerFactory.WaitForCacheSync(ctx.Done())
	}

	c.log.Info("grafana converter started")
	return nil
}

func ReadConfig(path string) (*ConverterConfig, error) {
	converterConfig := &ConverterConfig{}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConverterConfig{}, nil
		}
		return &ConverterConfig{}, err
	}
	if err = yaml.NewYAMLOrJSONDecoder(bufio.NewReader(f), 100).Decode(&converterConfig); err != nil {
		return &ConverterConfig{}, err
	}
	return converterConfig, nil
}

var (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which specifies the Namespace to watch.
	// An empty value means the operator is running with cluster scope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"

	validNamespaceRegex = regexp.MustCompile(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`)
	opNamespace         []string
	initNamespace       sync.Once
)

func getWatchNamespaces() ([]string, error) {
	wns, _ := os.LookupEnv(WatchNamespaceEnvVar)
	if len(wns) > 0 {
		nss := strings.Split(wns, ",")
		// validate namespace with regexp
		for _, ns := range nss {
			if !validNamespaceRegex.MatchString(ns) {
				return nil, fmt.Errorf("incorrect namespace name=%q for env var=%q with value: %q must match regex: %q", ns, WatchNamespaceEnvVar, wns, validNamespaceRegex.String())
			}
		}

		return nss, nil
	}
	return nil, nil
}

// MustGetWatchNamespaces returns a list of namespaces to be watched by operator
// Operator don't perform any cluster wide API calls if namespaces not empty
// in case of empty list it performs only cluster-wide api calls
func mustGetWatchNamespaces() []string {
	initNamespace.Do(func() {
		nss, err := getWatchNamespaces()
		if err != nil {
			panic(err)
		}
		opNamespace = nss
	})

	return opNamespace
}
