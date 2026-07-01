package source

import (
	"testing"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func TestBuildHelmRepository(t *testing.T) {
	relInterval := metav1.Duration{Duration: 5 * time.Minute}

	tests := []struct {
		name  string
		rel   *nelmv1alpha1.Release
		repo  *nelmv1alpha1.HelmRepositoryChartSource
		check func(t *testing.T, out *sourcev1.HelmRepository)
	}{
		{
			name: "https URL yields default type and maps every field",
			rel:  newRelease(testNS, "myrel", relInterval),
			repo: &nelmv1alpha1.HelmRepositoryChartSource{
				URL:             "https://charts.example.com",
				CredentialsFrom: &nelmv1alpha1.CredentialReference{Kind: "Secret", Name: "repo-secret"},
				CertificateFrom: &nelmv1alpha1.CertificateReference{Name: testCertSecret},
				PassCredentials: true,
				Insecure:        true,
				Interval:        metav1.Duration{Duration: 2 * time.Minute},
				Timeout:         &metav1.Duration{Duration: 90 * time.Second},
			},
			check: func(t *testing.T, out *sourcev1.HelmRepository) {
				if out.APIVersion != sourcev1.GroupVersion.String() {
					t.Errorf("APIVersion = %q, want %q", out.APIVersion, sourcev1.GroupVersion.String())
				}
				if out.Kind != sourcev1.HelmRepositoryKind {
					t.Errorf("Kind = %q, want %q", out.Kind, sourcev1.HelmRepositoryKind)
				}
				if out.Name != testGitRelName {
					t.Errorf("Name = %q, want apps-myrel", out.Name)
				}
				if out.Namespace != testNS {
					t.Errorf("Namespace = %q, want apps", out.Namespace)
				}
				if out.Spec.URL != "https://charts.example.com" {
					t.Errorf("URL = %q", out.Spec.URL)
				}
				if out.Spec.Type != sourcev1.HelmRepositoryTypeDefault {
					t.Errorf("Type = %q, want %q", out.Spec.Type, sourcev1.HelmRepositoryTypeDefault)
				}
				if out.Spec.SecretRef == nil || out.Spec.SecretRef.Name != "repo-secret" {
					t.Errorf("SecretRef = %+v, want name repo-secret", out.Spec.SecretRef)
				}
				if out.Spec.CertSecretRef == nil || out.Spec.CertSecretRef.Name != testCertSecret {
					t.Errorf("CertSecretRef = %+v, want name cert-secret", out.Spec.CertSecretRef)
				}
				if !out.Spec.PassCredentials {
					t.Error("PassCredentials = false, want true")
				}
				if !out.Spec.Insecure {
					t.Error("Insecure = false, want true")
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
			name: "oci URL yields oci type",
			rel:  newRelease(testNS, "ocirel", relInterval),
			repo: &nelmv1alpha1.HelmRepositoryChartSource{
				URL: "oci://registry.example.com/charts",
			},
			check: func(t *testing.T, out *sourcev1.HelmRepository) {
				if out.Spec.Type != sourcev1.HelmRepositoryTypeOCI {
					t.Errorf("Type = %q, want %q", out.Spec.Type, sourcev1.HelmRepositoryTypeOCI)
				}
				if out.Spec.URL != "oci://registry.example.com/charts" {
					t.Errorf("URL = %q", out.Spec.URL)
				}
			},
		},
		{
			name: "zero source interval inherits release interval and leaves optionals nil",
			rel:  newRelease("default", "minimal", relInterval),
			repo: &nelmv1alpha1.HelmRepositoryChartSource{
				URL: "https://charts.example.com",
			},
			check: func(t *testing.T, out *sourcev1.HelmRepository) {
				if out.Name != testMinimalName {
					t.Errorf("Name = %q, want default-minimal", out.Name)
				}
				if out.Namespace != "default" {
					t.Errorf("Namespace = %q, want default", out.Namespace)
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
				if out.Spec.Timeout != nil {
					t.Errorf("Timeout = %v, want nil", out.Spec.Timeout)
				}
				if out.Spec.PassCredentials {
					t.Error("PassCredentials = true, want false")
				}
				if out.Spec.Insecure {
					t.Error("Insecure = true, want false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := buildHelmRepository(tt.rel, tt.repo)
			tt.check(t, out)
		})
	}
}
