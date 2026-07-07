package v1alpha1

const (
	ReleaseFinalizerName = "nelm.werf.io/release"

	HelmChartReleaseRefLabelName      = "nelm.werf.io/release-ref"
	SourceRefReleaseRefAnnotationName = "nelm.werf.io/release-refs"

	ChartSourcePhaseReady         = "Ready"
	ChartSourcePhasePending       = "Pending"
	ChartSourcePhaseDisconnecting = "Disconnecting"
)
