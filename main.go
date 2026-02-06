package main

import (
	"context"
	"os"

	"github.com/Netcracker/qubership-grafana-operator-converter/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	ctrl "sigs.k8s.io/controller-runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	stop := signals.SetupSignalHandler()
	go func() {
		<-stop.Done()
		cancel()
	}()

	err := manager.RunManager(ctx)
	if err != nil {
		setupLog.Error(err, "manager exited with error")
		os.Exit(1)
	}
	setupLog.Info("stopped converter")
}
