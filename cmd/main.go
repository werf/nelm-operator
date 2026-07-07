package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm-operator/internal/config"
	"github.com/werf/nelm-operator/internal/controller"
	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/featgate"
	"github.com/werf/nelm/pkg/log"
)

var (
	scheme   = k8sruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(nelmv1alpha1.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
}

func main() {
	var cfg config.OperatorConfig
	var metricsCertDir, metricsCertName, metricsCertKey string

	// Controller runtime flags.
	flag.StringVar(&cfg.MetricsBindAddress, "metrics-bind-address", "0", "The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable.")
	flag.StringVar(&cfg.HealthProbeBindAddress, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&cfg.LeaderElect, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&cfg.MetricsSecure, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS.")
	flag.StringVar(&metricsCertDir, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")

	// Operator-specific flags.
	flag.IntVar(&cfg.MaxConcurrentReconciles, "max-concurrent-reconciles", 5, "Number of Release CRDs reconciled in parallel.")
	flag.DurationVar(&cfg.GracefulShutdownTimeout, "graceful-shutdown-timeout", 600*time.Second, "How long to wait for in-flight reconciles on SIGTERM.")
	flag.BoolVar(&cfg.WatchAllNamespaces, "watch-all-namespaces", true, "Watch Release CRDs in all namespaces.")
	flag.StringVar(&cfg.WatchNamespace, "watch-namespace", "", "If set, only watch this namespace for Release CRDs.")
	// Source controller integration.
	flag.StringVar(&cfg.SourceAPIGroup, "source-api-group", "source.toolkit.fluxcd.io",
		"API group for spec.chartRef sources; inline spec.chart always uses source.toolkit.fluxcd.io.")
	flag.StringVar(&cfg.SourceAPIVersion, "source-api-version", "v1",
		"API version for spec.chartRef sources; inline spec.chart always uses v1.")
	flag.IntVar(&cfg.HTTPRetry, "http-retry", 9, "Number of retries when downloading chart artifacts.")
	flag.DurationVar(&cfg.HTTPTimeout, "http-timeout", 30*time.Second, "Timeout for downloading chart artifacts.")

	// Release storage.
	flag.StringVar(&cfg.ReleaseStorageDriver, "release-storage-driver", "secret", "How Helm release metadata is stored: secret, configmap, sql.")
	flag.StringVar(&cfg.ReleaseStorageSQLConnection, "release-storage-sql-connection", "", "SQL connection string when using sql storage driver.")

	// Kubernetes API.
	flag.IntVar(&cfg.KubeQPSLimit, "kube-qps-limit", 50, "QPS limit for requests to Kubernetes API.")
	flag.IntVar(&cfg.KubeBurstLimit, "kube-burst-limit", 100, "Burst limit for requests to Kubernetes API.")
	flag.DurationVar(&cfg.KubeRequestTimeout, "kube-request-timeout", 0, "Timeout for individual requests to Kubernetes API. 0 = no timeout.")
	flag.IntVar(&cfg.NetworkParallelism, "network-parallelism", 30, "Limit of network-related tasks to run in parallel per reconcile.")

	// TypeScript / Misc.
	flag.StringVar(&cfg.DenoBinaryPath, "deno-binary-path", "", "Path to the Deno binary for TypeScript chart rendering.")
	flag.StringVar(&cfg.TempDir, "temp-dir", "", "Directory for temporary files.")

	// Logging.
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "Log level: silent, error, warning, info, debug, trace.")

	opts := zap.Options{Development: cfg.LogLevel == "debug" || cfg.LogLevel == "trace"}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	action.SetupLogging(context.Background(), log.Level(cfg.LogLevel), action.SetupLoggingOptions{
		ColorMode: log.LogColorModeOff,
	})

	if featgate.FeatGatePeriodicStackTraces.Enabled() {
		go func() {
			for {
				buf := make([]byte, 1<<20)
				runtime.Stack(buf, true)
				fmt.Printf("%s", buf)
				time.Sleep(10 * time.Second)
			}
		}()
	}

	if cfg.MaxConcurrentReconciles > 1 {
		setupLog.Info("WARNING: --max-concurrent-reconciles > 1 is unsafe when releases use different secretKeyFrom values due to a process-global WERF_SECRET_KEY env var race in the nelm library")
	}

	if v := os.Getenv("NELM_RELEASE_STORAGE_SQL_CONNECTION"); v != "" && cfg.ReleaseStorageSQLConnection == "" {
		cfg.ReleaseStorageSQLConnection = v
	}

	cfg.MetricsCertDir = metricsCertDir
	cfg.MetricsCertName = metricsCertName
	cfg.MetricsCertKey = metricsCertKey

	// Apply QPS/Burst to controller-runtime's rest config.
	restConfig := ctrl.GetConfigOrDie()
	restConfig.QPS = float32(cfg.KubeQPSLimit)
	restConfig.Burst = cfg.KubeBurstLimit
	if cfg.KubeRequestTimeout > 0 {
		restConfig.Timeout = cfg.KubeRequestTimeout
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   cfg.MetricsBindAddress,
		SecureServing: cfg.MetricsSecure,
	}

	if cfg.MetricsSecure {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if metricsCertDir != "" {
		metricsServerOptions.CertDir = metricsCertDir
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgrOptions := ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsServerOptions,
		HealthProbeBindAddress:  cfg.HealthProbeBindAddress,
		LeaderElection:          cfg.LeaderElect,
		LeaderElectionID:        "50578024.werf.io",
		GracefulShutdownTimeout: &cfg.GracefulShutdownTimeout,
	}

	// Namespace filtering.
	if cfg.WatchNamespace != "" {
		mgrOptions.Cache.DefaultNamespaces = map[string]cache.Config{
			cfg.WatchNamespace: {},
		}
	} else if !cfg.WatchAllNamespaces {
		setupLog.Error(nil, "--watch-all-namespaces=false requires --watch-namespace to be set")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(restConfig, mgrOptions)
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	if err := (&controller.ReleaseReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: cfg,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "release")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(action.SetupLogging(ctrl.SetupSignalHandler(), log.Level(cfg.LogLevel), action.SetupLoggingOptions{
		ColorMode: log.LogColorModeOff,
	})); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}
