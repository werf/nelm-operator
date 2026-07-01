package source

import (
	"reflect"
	"testing"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func TestBuildHelmChart(t *testing.T) { //nolint:gocyclo // table-driven field assertions
	relInterval := metav1.Duration{Duration: 5 * time.Minute}
	verification := &sourcev1.OCIRepositoryVerification{
		Provider:  "cosign",
		SecretRef: nil,
	}

	tests := []struct {
		name  string
		build func(rel *nelmv1alpha1.Release) *sourcev1.HelmChart
		rel   *nelmv1alpha1.Release
		check func(t *testing.T, out *sourcev1.HelmChart)
	}{
		{
			name: "repo source maps chart, version and verify",
			rel:  newRelease(testNS, "myrel", relInterval),
			build: func(rel *nelmv1alpha1.Release) *sourcev1.HelmChart {
				return buildHelmChartForHelmRepositorySource(rel, &nelmv1alpha1.HelmRepositoryChartSource{
					Name:                     "podinfo",
					Version:                  "6.7.1",
					ValuesFiles:              []string{"values-prod.yaml", "override.yaml"},
					IgnoreMissingValuesFiles: true,
					Verify:                   verification,
					Interval:                 metav1.Duration{Duration: 2 * time.Minute},
				})
			},
			check: func(t *testing.T, out *sourcev1.HelmChart) {
				if out.Spec.Chart != "podinfo" {
					t.Errorf("Chart = %q, want podinfo", out.Spec.Chart)
				}
				if out.Spec.Version != "6.7.1" {
					t.Errorf("Version = %q, want 6.7.1", out.Spec.Version)
				}
				if out.Spec.SourceRef.Kind != sourcev1.HelmRepositoryKind {
					t.Errorf("SourceRef.Kind = %q, want %q", out.Spec.SourceRef.Kind, sourcev1.HelmRepositoryKind)
				}
				if out.Spec.SourceRef.Name != testGitRelName {
					t.Errorf("SourceRef.Name = %q, want apps-myrel", out.Spec.SourceRef.Name)
				}
				if out.Spec.Verify != verification {
					t.Errorf("Verify = %+v, want the provided verification", out.Spec.Verify)
				}
				if !reflect.DeepEqual(out.Spec.ValuesFiles, []string{"values-prod.yaml", "override.yaml"}) {
					t.Errorf("ValuesFiles = %v", out.Spec.ValuesFiles)
				}
				if !out.Spec.IgnoreMissingValuesFiles {
					t.Error("IgnoreMissingValuesFiles = false, want true")
				}
				// Interval is set on the source, so it must NOT inherit the Release interval.
				if out.Spec.Interval != (metav1.Duration{Duration: 2 * time.Minute}) {
					t.Errorf("Interval = %v, want 2m", out.Spec.Interval)
				}
			},
		},
		{
			name: "git source uses path, empty version and nil verify",
			rel:  newRelease(testNS, "gitrel", relInterval),
			build: func(rel *nelmv1alpha1.Release) *sourcev1.HelmChart {
				return buildHelmChartForGitRepositorySource(rel, &nelmv1alpha1.GitRepositoryChartSource{
					URL:                      "https://github.com/example/repo.git",
					Path:                     "charts/app",
					ValuesFiles:              []string{"values.yaml"},
					IgnoreMissingValuesFiles: true,
					Interval:                 metav1.Duration{Duration: 3 * time.Minute},
				})
			},
			check: func(t *testing.T, out *sourcev1.HelmChart) {
				if out.Spec.Chart != "charts/app" {
					t.Errorf("Chart = %q, want charts/app", out.Spec.Chart)
				}
				if out.Spec.Version != "" {
					t.Errorf("Version = %q, want empty (ignored for Git)", out.Spec.Version)
				}
				if out.Spec.SourceRef.Kind != sourcev1.GitRepositoryKind {
					t.Errorf("SourceRef.Kind = %q, want %q", out.Spec.SourceRef.Kind, sourcev1.GitRepositoryKind)
				}
				if out.Spec.Verify != nil {
					t.Errorf("Verify = %+v, want nil for Git source", out.Spec.Verify)
				}
				if !reflect.DeepEqual(out.Spec.ValuesFiles, []string{"values.yaml"}) {
					t.Errorf("ValuesFiles = %v", out.Spec.ValuesFiles)
				}
				if !out.Spec.IgnoreMissingValuesFiles {
					t.Error("IgnoreMissingValuesFiles = false, want true")
				}
			},
		},
		{
			name: "bucket source uses path, empty version and nil verify",
			rel:  newRelease(testNS, "bucketrel", relInterval),
			build: func(rel *nelmv1alpha1.Release) *sourcev1.HelmChart {
				return buildHelmChartForBucketSource(rel, &nelmv1alpha1.BucketChartSource{
					Endpoint:                 "s3.example.com",
					BucketName:               "charts",
					Path:                     "stable/app",
					ValuesFiles:              []string{"prod.yaml"},
					IgnoreMissingValuesFiles: true,
					Interval:                 metav1.Duration{Duration: 4 * time.Minute},
				})
			},
			check: func(t *testing.T, out *sourcev1.HelmChart) {
				if out.Spec.Chart != "stable/app" {
					t.Errorf("Chart = %q, want stable/app", out.Spec.Chart)
				}
				if out.Spec.Version != "" {
					t.Errorf("Version = %q, want empty (ignored for Bucket)", out.Spec.Version)
				}
				if out.Spec.SourceRef.Kind != sourcev1.BucketKind {
					t.Errorf("SourceRef.Kind = %q, want %q", out.Spec.SourceRef.Kind, sourcev1.BucketKind)
				}
				if out.Spec.Verify != nil {
					t.Errorf("Verify = %+v, want nil for Bucket source", out.Spec.Verify)
				}
				if !reflect.DeepEqual(out.Spec.ValuesFiles, []string{"prod.yaml"}) {
					t.Errorf("ValuesFiles = %v", out.Spec.ValuesFiles)
				}
			},
		},
		{
			name: "repo source with zero interval inherits release interval",
			rel:  newRelease("default", "minimal", relInterval),
			build: func(rel *nelmv1alpha1.Release) *sourcev1.HelmChart {
				return buildHelmChartForHelmRepositorySource(rel, &nelmv1alpha1.HelmRepositoryChartSource{
					Name: "podinfo",
				})
			},
			check: func(t *testing.T, out *sourcev1.HelmChart) {
				// Zero source interval must inherit the Release-level interval.
				if out.Spec.Interval != relInterval {
					t.Errorf("Interval = %v, want injected %v", out.Spec.Interval, relInterval)
				}
				if out.Spec.Verify != nil {
					t.Errorf("Verify = %+v, want nil when not provided", out.Spec.Verify)
				}
			},
		},
		{
			name: "core nils verify for non-HelmRepository kind defensively",
			rel:  newRelease(testNS, "defensive", relInterval),
			build: func(rel *nelmv1alpha1.Release) *sourcev1.HelmChart {
				// Call the core directly with a GitRepository kind AND a non-nil
				// verify: the XValidation guardrail must strip it.
				return buildHelmChart(
					rel,
					sourcev1.GitRepositoryKind,
					"charts/app",
					"1.2.3",
					metav1.Duration{},
					nil,
					false,
					verification,
					sourcev1.ReconcileStrategyChartVersion,
				)
			},
			check: func(t *testing.T, out *sourcev1.HelmChart) {
				if out.Spec.Verify != nil {
					t.Errorf("Verify = %+v, want nil (core must drop it for non-HelmRepository)", out.Spec.Verify)
				}
				if out.Spec.SourceRef.Kind != sourcev1.GitRepositoryKind {
					t.Errorf("SourceRef.Kind = %q, want %q", out.Spec.SourceRef.Kind, sourcev1.GitRepositoryKind)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.build(tt.rel)

			// Invariants shared across every source kind.
			if out.APIVersion != sourcev1.GroupVersion.String() {
				t.Errorf("APIVersion = %q, want %q", out.APIVersion, sourcev1.GroupVersion.String())
			}
			if out.Kind != sourcev1.HelmChartKind {
				t.Errorf("Kind = %q, want %q", out.Kind, sourcev1.HelmChartKind)
			}
			wantName := tt.rel.Namespace + "-" + tt.rel.Name
			if out.Name != wantName {
				t.Errorf("Name = %q, want %q", out.Name, wantName)
			}
			if out.Namespace != tt.rel.Namespace {
				t.Errorf("Namespace = %q, want %q", out.Namespace, tt.rel.Namespace)
			}
			if out.Spec.SourceRef.APIVersion != sourcev1.GroupVersion.String() {
				t.Errorf("SourceRef.APIVersion = %q, want %q", out.Spec.SourceRef.APIVersion, sourcev1.GroupVersion.String())
			}
			if out.Spec.SourceRef.Name != wantName {
				t.Errorf("SourceRef.Name = %q, want %q", out.Spec.SourceRef.Name, wantName)
			}

			tt.check(t, out)
		})
	}
}
