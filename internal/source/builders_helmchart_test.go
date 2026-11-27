package source

import (
	"slices"
	"testing"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func TestBuildHelmChartPassesValuesFilesToFlux(t *testing.T) {
	valuesFiles := []string{"values.yaml", "values-prod.yaml"}
	rel := &nelmv1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"}}

	tests := []struct {
		name  string
		build func() *sourcev1.HelmChart
	}{
		{
			name: "repo",
			build: func() *sourcev1.HelmChart {
				return BuildHelmChartForHelmRepositorySource("source.toolkit.fluxcd.io", "v1", rel, "src", &nelmv1alpha1.HelmRepositoryChartSource{
					ValuesFiles:              valuesFiles,
					IgnoreMissingValuesFiles: true,
				})
			},
		},
		{
			name: "git",
			build: func() *sourcev1.HelmChart {
				return BuildHelmChartForGitRepositorySource("source.toolkit.fluxcd.io", "v1", rel, "src", &nelmv1alpha1.GitRepositoryChartSource{
					ValuesFiles:              valuesFiles,
					IgnoreMissingValuesFiles: true,
				})
			},
		},
		{
			name: "bucket",
			build: func() *sourcev1.HelmChart {
				return BuildHelmChartForBucketSource("source.toolkit.fluxcd.io", "v1", rel, "src", &nelmv1alpha1.BucketChartSource{
					ValuesFiles:              valuesFiles,
					IgnoreMissingValuesFiles: true,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := tt.build()
			if !slices.Equal(hc.Spec.ValuesFiles, valuesFiles) {
				t.Errorf("spec.valuesFiles = %v, want %v", hc.Spec.ValuesFiles, valuesFiles)
			}
			if !hc.Spec.IgnoreMissingValuesFiles {
				t.Errorf("spec.ignoreMissingValuesFiles = false, want true")
			}
		})
	}
}
