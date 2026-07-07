package source

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func buildHelmChart(
	sourceAPIGroup string,
	sourceAPIVersion string,
	rel *nelmv1alpha1.Release,
	sourceKind, sourceName, chart, version string,
	interval metav1.Duration,
	valuesFiles []string,
	ignoreMissing bool,
	verify *sourcev1.OCIRepositoryVerification,
	reconcileStrategy string,
) *sourcev1.HelmChart {
	// TODO: double check on object naming
	name := GetHelmChartHashedName(rel.Namespace, rel.Name)

	// Defensive guardrail for the XValidation above: drop Verify unless the
	// source is a HelmRepository.
	if sourceKind != sourcev1.HelmRepositoryKind {
		verify = nil
	}

	return &sourcev1.HelmChart{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourceAPIGroup + "/" + sourceAPIVersion,
			Kind:       sourcev1.HelmChartKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: rel.Namespace,
			Labels: map[string]string{
				nelmv1alpha1.HelmChartReleaseRefLabelName: rel.Name,
			},
		},
		Spec: sourcev1.HelmChartSpec{
			Chart:   chart,
			Version: version,
			SourceRef: sourcev1.LocalHelmChartSourceReference{
				APIVersion: sourcev1.GroupVersion.String(),
				Kind:       sourceKind,
				Name:       sourceName,
			},
			Interval:                 getNoneZeroDuration(interval, rel.Spec.Interval),
			ValuesFiles:              valuesFiles,
			IgnoreMissingValuesFiles: ignoreMissing,
			Verify:                   verify,
			ReconcileStrategy:        reconcileStrategy,
		},
	}
}

func GetHelmChartHashedName(releaseNamespace, releaseName string) string {
	namePrefix := fmt.Sprintf("%s-inline-%s", releaseNamespace, releaseName)
	hash := sha256.Sum256([]byte(namePrefix))
	return namePrefix + "-" + hex.EncodeToString(hash[:])[:12]
}

// BuildHelmChartForHelmRepositorySource builds the HelmChart for a HelmRepository-backed chart.
// The chart name and version come from the Helm repository source, and Verify
// is forwarded since it is only valid for HelmRepository sources.
func BuildHelmChartForHelmRepositorySource(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release, sourceName string, repo *nelmv1alpha1.HelmRepositoryChartSource) *sourcev1.HelmChart {
	return buildHelmChart(
		sourceAPIGroup,
		sourceAPIVersion,
		rel,
		sourcev1.HelmRepositoryKind,
		sourceName,
		repo.Name,
		repo.Version,
		repo.Interval,
		repo.ValuesFiles,
		repo.IgnoreMissingValuesFiles,
		repo.Verify,
		sourcev1.ReconcileStrategyChartVersion,
	)
}

// BuildHelmChartForGitRepositorySource builds the HelmChart for a GitRepository-backed chart.
// The chart is addressed by Path; Version is ignored by FluxCD for Git sources
// and Verify is not permitted, so both are left empty/nil.
func BuildHelmChartForGitRepositorySource(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release, sourceName string, git *nelmv1alpha1.GitRepositoryChartSource) *sourcev1.HelmChart {
	return buildHelmChart(
		sourceAPIGroup,
		sourceAPIVersion,
		rel,
		sourcev1.GitRepositoryKind,
		sourceName,
		git.Path,
		"",
		git.Interval,
		git.ValuesFiles,
		git.IgnoreMissingValuesFiles,
		nil,
		git.ReconcileStrategy,
	)
}

// BuildHelmChartForBucketSource builds the HelmChart for a Bucket-backed chart.
// The chart is addressed by Path; Version is ignored by FluxCD for Bucket
// sources and Verify is not permitted, so both are left empty/nil.
func BuildHelmChartForBucketSource(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release, sourceName string, bucket *nelmv1alpha1.BucketChartSource) *sourcev1.HelmChart {
	return buildHelmChart(
		sourceAPIGroup,
		sourceAPIVersion,
		rel,
		sourcev1.BucketKind,
		sourceName,
		bucket.Path,
		"",
		bucket.Interval,
		bucket.ValuesFiles,
		bucket.IgnoreMissingValuesFiles,
		nil,
		bucket.ReconcileStrategy,
	)
}
