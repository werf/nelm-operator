package source

import (
	"testing"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func TestBuildOCIRepository(t *testing.T) { //nolint:gocyclo // table-driven field assertions
	relInterval := metav1.Duration{Duration: 5 * time.Minute}
	verify := &sourcev1.OCIRepositoryVerification{Provider: "cosign"}

	tests := []struct {
		name  string
		rel   *nelmv1alpha1.Release
		oci   *nelmv1alpha1.OCIRepositoryChartSource
		check func(t *testing.T, in *nelmv1alpha1.OCIRepositoryChartSource, out *sourcev1.OCIRepository)
	}{
		{
			name: "fully populated source maps every field",
			rel:  newRelease(testNS, "myrel", relInterval),
			oci: &nelmv1alpha1.OCIRepositoryChartSource{
				URL:                "oci://registry.example.com/charts/app",
				Tag:                "v1.0.0",
				CredentialsFrom:    &nelmv1alpha1.CredentialReference{Kind: "Secret", Name: "oci-secret"},
				CertificateFrom:    &nelmv1alpha1.CertificateReference{Name: testCertSecret},
				ProxySettingsFrom:  &nelmv1alpha1.ProxySettingsReference{Kind: "Secret", Name: testProxySecret},
				ServiceAccountName: "oci-sa",
				Provider:           testProviderAWS,
				Insecure:           true,
				Ignore:             ptr(testIgnoreGlob),
				LayerSelector:      &sourcev1.OCILayerSelector{MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip"},
				Verify:             verify,
				Interval:           metav1.Duration{Duration: 2 * time.Minute},
				Timeout:            &metav1.Duration{Duration: 90 * time.Second},
			},
			check: func(t *testing.T, in *nelmv1alpha1.OCIRepositoryChartSource, out *sourcev1.OCIRepository) {
				if out.APIVersion != sourcev1.GroupVersion.String() {
					t.Errorf("APIVersion = %q, want %q", out.APIVersion, sourcev1.GroupVersion.String())
				}
				if out.Kind != sourcev1.OCIRepositoryKind {
					t.Errorf("Kind = %q, want %q", out.Kind, sourcev1.OCIRepositoryKind)
				}
				if out.Name != testGitRelName {
					t.Errorf("Name = %q, want apps-myrel", out.Name)
				}
				if out.Namespace != testNS {
					t.Errorf("Namespace = %q, want apps", out.Namespace)
				}
				if out.Spec.URL != "oci://registry.example.com/charts/app" {
					t.Errorf("URL = %q", out.Spec.URL)
				}
				if out.Spec.Reference == nil {
					t.Fatal("Reference is nil, want populated OCIRepositoryRef")
				}
				if out.Spec.Reference.Tag != in.Tag {
					t.Errorf("Reference.Tag = %q, want %q", out.Spec.Reference.Tag, in.Tag)
				}
				if out.Spec.SecretRef == nil || out.Spec.SecretRef.Name != "oci-secret" {
					t.Errorf("SecretRef = %+v, want name oci-secret", out.Spec.SecretRef)
				}
				if out.Spec.CertSecretRef == nil || out.Spec.CertSecretRef.Name != testCertSecret {
					t.Errorf("CertSecretRef = %+v, want name cert-secret", out.Spec.CertSecretRef)
				}
				if out.Spec.ProxySecretRef == nil || out.Spec.ProxySecretRef.Name != testProxySecret {
					t.Errorf("ProxySecretRef = %+v, want name proxy-secret", out.Spec.ProxySecretRef)
				}
				if out.Spec.ServiceAccountName != "oci-sa" {
					t.Errorf("ServiceAccountName = %q, want oci-sa", out.Spec.ServiceAccountName)
				}
				if out.Spec.Provider != testProviderAWS {
					t.Errorf("Provider = %q, want aws (passthrough)", out.Spec.Provider)
				}
				if !out.Spec.Insecure {
					t.Error("Insecure = false, want true")
				}
				if out.Spec.Ignore == nil || *out.Spec.Ignore != testIgnoreGlob {
					t.Errorf("Ignore = %v, want *.md", out.Spec.Ignore)
				}
				if out.Spec.LayerSelector == nil || out.Spec.LayerSelector.MediaType != "application/vnd.cncf.helm.chart.content.v1.tar+gzip" {
					t.Errorf("LayerSelector = %+v", out.Spec.LayerSelector)
				}
				// Verify types match between nelm and FluxCD, so the same pointer passes through.
				if out.Spec.Verify != in.Verify {
					t.Errorf("Verify = %p, want same pointer %p", out.Spec.Verify, in.Verify)
				}
				// Interval is set on the source, so it must NOT be overridden by the Release interval.
				if out.Spec.Interval != (metav1.Duration{Duration: 2 * time.Minute}) {
					t.Errorf("Interval = %v, want 2m", out.Spec.Interval)
				}
				if out.Spec.Timeout == nil || out.Spec.Timeout.Duration != 90*time.Second {
					t.Errorf("Timeout = %v, want 90s", out.Spec.Timeout)
				}
			},
		},
		{
			name: "semver and filter propagate into nested ref",
			rel:  newRelease(testNS, "semverrel", relInterval),
			oci: &nelmv1alpha1.OCIRepositoryChartSource{
				URL:          "oci://registry.example.com/charts/app",
				SemVer:       ">=1.0.0 <2.0.0",
				SemverFilter: "^v.*",
			},
			check: func(t *testing.T, in *nelmv1alpha1.OCIRepositoryChartSource, out *sourcev1.OCIRepository) {
				if out.Spec.Reference == nil {
					t.Fatal("Reference is nil, want populated OCIRepositoryRef")
				}
				if out.Spec.Reference.SemVer != in.SemVer {
					t.Errorf("Reference.SemVer = %q, want %q", out.Spec.Reference.SemVer, in.SemVer)
				}
				if out.Spec.Reference.SemverFilter != in.SemverFilter {
					t.Errorf("Reference.SemverFilter = %q, want %q", out.Spec.Reference.SemverFilter, in.SemverFilter)
				}
			},
		},
		{
			name: "digest propagates into nested ref",
			rel:  newRelease(testNS, "digestrel", relInterval),
			oci: &nelmv1alpha1.OCIRepositoryChartSource{
				URL:    "oci://registry.example.com/charts/app",
				Digest: "sha256:1234567890abcdef",
			},
			check: func(t *testing.T, in *nelmv1alpha1.OCIRepositoryChartSource, out *sourcev1.OCIRepository) {
				if out.Spec.Reference == nil {
					t.Fatal("Reference is nil, want populated OCIRepositoryRef")
				}
				if out.Spec.Reference.Digest != in.Digest {
					t.Errorf("Reference.Digest = %q, want %q", out.Spec.Reference.Digest, in.Digest)
				}
			},
		},
		{
			name: "all ref selectors empty leaves reference nil and inherits release interval",
			rel:  newRelease("default", "minimal", relInterval),
			oci: &nelmv1alpha1.OCIRepositoryChartSource{
				URL: "oci://registry.example.com/charts/app",
			},
			check: func(t *testing.T, in *nelmv1alpha1.OCIRepositoryChartSource, out *sourcev1.OCIRepository) {
				if out.Name != testMinimalName {
					t.Errorf("Name = %q, want default-minimal", out.Name)
				}
				// All four selectors empty must collapse to a nil reference, never an all-empty ref.
				if out.Spec.Reference != nil {
					t.Errorf("Reference = %+v, want nil", out.Spec.Reference)
				}
				// Zero source interval must inherit the Release-level interval.
				if out.Spec.Interval != relInterval {
					t.Errorf("Interval = %v, want injected %v", out.Spec.Interval, relInterval)
				}
				if out.Spec.SecretRef != nil {
					t.Errorf("SecretRef = %+v, want nil", out.Spec.SecretRef)
				}
				if out.Spec.CertSecretRef != nil {
					t.Errorf("CertSecretRef = %+v, want nil", out.Spec.CertSecretRef)
				}
				if out.Spec.ProxySecretRef != nil {
					t.Errorf("ProxySecretRef = %+v, want nil", out.Spec.ProxySecretRef)
				}
				if out.Spec.Timeout != nil {
					t.Errorf("Timeout = %v, want nil", out.Spec.Timeout)
				}
				if out.Spec.Ignore != nil {
					t.Errorf("Ignore = %v, want nil", out.Spec.Ignore)
				}
				if out.Spec.LayerSelector != nil {
					t.Errorf("LayerSelector = %+v, want nil", out.Spec.LayerSelector)
				}
				if out.Spec.Verify != nil {
					t.Errorf("Verify = %+v, want nil", out.Spec.Verify)
				}
				if out.Spec.Insecure {
					t.Error("Insecure = true, want false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := buildOCIRepository(tt.rel, tt.oci)
			tt.check(t, tt.oci, out)
		})
	}
}
