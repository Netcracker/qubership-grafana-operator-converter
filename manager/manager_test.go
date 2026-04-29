package manager

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

func TestGetNamespaceConfig(t *testing.T) {
	config := getNamespaceConfig("team-a,team-b")
	require.Len(t, config, 2)

	for _, ns := range []string{"team-a", "team-b"} {
		nsConfig, ok := config[ns]
		require.True(t, ok)
		assert.Equal(t, labels.Everything().String(), nsConfig.LabelSelector.String())
		assert.Equal(t, fields.Everything().String(), nsConfig.FieldSelector.String())
		assert.Nil(t, nsConfig.Transform)
		assert.Nil(t, nsConfig.UnsafeDisableDeepCopy)
	}
}

func TestRunManagerReturnsErrorForInvalidKubeconfig(t *testing.T) {
	oldKubeconfig := kubeconfig
	oldCommandLine := flag.CommandLine
	defer func() {
		kubeconfig = oldKubeconfig
		flag.CommandLine = oldCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	metricsAddr = flag.CommandLine.String("metrics-bind-address", ":8080", "")
	probeAddr = flag.CommandLine.String("health-probe-bind-address", ":8081", "")
	pprofEnabled = flag.CommandLine.Bool("pprof-enable", false, "")
	pprofAddr = flag.CommandLine.String("pprof-address", ":9180", "")
	enableLeaderElection = flag.CommandLine.Bool("leader-elect", false, "")
	resyncPeriod = flag.CommandLine.Duration("controller.resyncPeriod", 0, "")
	converterConfigPath = flag.CommandLine.String("controller.config", "/opt/grafana-converter/parameters.yaml", "")
	kubeconfig = flag.CommandLine.String("kubeconfig", "/definitely/missing/kubeconfig", "")

	err := RunManager(context.Background())
	require.Error(t, err)
}
