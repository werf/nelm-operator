package source

import (
	"fmt"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

// buildHelmChart maps the chart-scoped inputs of a nelm chart source onto a
// typed FluxCD HelmChart object. It is a pure function: it has no side effects
// and does not set owner references (that is handled by the ensure* layer).
//
// The HelmChart is the object that actually carries the chart name/path,
// version, values files and signature verification; the per-source object
// (GitRepository/HelmRepository/Bucket) only describes where to fetch from.
// The SourceRef ties the two together by name and kind.
//
// FluxCD's HelmChartSpec carries an XValidation rule:
//
//	!has(self.verify) || self.sourceRef.kind == 'HelmRepository'
//
// so Verify is only valid for HelmRepository-backed charts. The wrappers
// already pass nil Verify for git/bucket sources, but the core defensively
// nils it out for any non-HelmRepository kind so a malformed call can never
// produce a HelmChart the CRD would reject.
func buildHelmChart(
	rel *nelmv1alpha1.Release,
	sourceKind, chart, version string,
	interval metav1.Duration,
	valuesFiles []string,
	ignoreMissing bool,
	verify *sourcev1.OCIRepositoryVerification,
	reconcileStrategy string,
) *sourcev1.HelmChart {
	// TODO: double check on object naming
	name := fmt.Sprintf("%s-%s", rel.Namespace, rel.Name)

	// Defensive guardrail for the XValidation above: drop Verify unless the
	// source is a HelmRepository.
	if sourceKind != sourcev1.HelmRepositoryKind {
		verify = nil
	}

	return &sourcev1.HelmChart{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourcev1.GroupVersion.String(),
			Kind:       sourcev1.HelmChartKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: rel.Namespace,
		},
		Spec: sourcev1.HelmChartSpec{
			Chart:   chart,
			Version: version,
			SourceRef: sourcev1.LocalHelmChartSourceReference{
				APIVersion: sourcev1.GroupVersion.String(),
				Kind:       sourceKind,
				Name:       name,
			},
			Interval:                 getNoneZeroDuration(interval, rel.Spec.Interval),
			ValuesFiles:              valuesFiles,
			IgnoreMissingValuesFiles: ignoreMissing,
			Verify:                   verify,
			ReconcileStrategy:        reconcileStrategy,
		},
	}
}

// buildHelmChartForHelmRepositorySource builds the HelmChart for a HelmRepository-backed chart.
// The chart name and version come from the Helm repository source, and Verify
// is forwarded since it is only valid for HelmRepository sources.
func buildHelmChartForHelmRepositorySource(rel *nelmv1alpha1.Release, repo *nelmv1alpha1.HelmRepositoryChartSource) *sourcev1.HelmChart {
	return buildHelmChart(
		rel,
		sourcev1.HelmRepositoryKind,
		repo.Name,
		repo.Version,
		repo.Interval,
		repo.ValuesFiles,
		repo.IgnoreMissingValuesFiles,
		repo.Verify,
		sourcev1.ReconcileStrategyChartVersion,
	)
}

// buildHelmChartForGitRepositorySource builds the HelmChart for a GitRepository-backed chart.
// The chart is addressed by Path; Version is ignored by FluxCD for Git sources
// and Verify is not permitted, so both are left empty/nil.
func buildHelmChartForGitRepositorySource(rel *nelmv1alpha1.Release, git *nelmv1alpha1.GitRepositoryChartSource) *sourcev1.HelmChart {
	return buildHelmChart(
		rel,
		sourcev1.GitRepositoryKind,
		git.Path,
		"",
		git.Interval,
		git.ValuesFiles,
		git.IgnoreMissingValuesFiles,
		nil,
		git.ReconcileStrategy,
	)
}

// buildHelmChartForBucketSource builds the HelmChart for a Bucket-backed chart.
// The chart is addressed by Path; Version is ignored by FluxCD for Bucket
// sources and Verify is not permitted, so both are left empty/nil.
func buildHelmChartForBucketSource(rel *nelmv1alpha1.Release, bucket *nelmv1alpha1.BucketChartSource) *sourcev1.HelmChart {
	return buildHelmChart(
		rel,
		sourcev1.BucketKind,
		bucket.Path,
		"",
		bucket.Interval,
		bucket.ValuesFiles,
		bucket.IgnoreMissingValuesFiles,
		nil,
		bucket.ReconcileStrategy,
	)
}
