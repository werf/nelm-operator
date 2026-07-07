package config

import (
	"time"

	"k8s.io/apimachinery/pkg/labels"
)

type OperatorConfig struct {
	DefaultServiceAccountName string

	LeaderElect                  bool
	MaxConcurrentReconciles      int
	MetricsBindAddress           string
	HealthProbeBindAddress       string
	MetricsSecure                bool
	MetricsCertDir               string
	MetricsCertName              string
	MetricsCertKey               string
	GracefulShutdownTimeout      time.Duration
	WatchAllNamespaces           bool
	WatchNamespace               string
	DependencyWatchLabelSelector labels.Selector

	SourceAPIGroup   string
	SourceAPIVersion string
	HTTPRetry        int
	HTTPTimeout      time.Duration

	ReleaseStorageDriver        string
	ReleaseStorageSQLConnection string

	KubeQPSLimit       int
	KubeBurstLimit     int
	KubeRequestTimeout time.Duration
	NetworkParallelism int

	DenoBinaryPath string
	TempDir        string

	LogLevel string
}
