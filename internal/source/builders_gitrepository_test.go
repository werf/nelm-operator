package source

import (
	"reflect"
	"testing"
	"time"

	meta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

const (
	testNS          = "apps"
	testGitRelName  = "apps-myrel"
	testProxySecret = "proxy-secret"
	testCertSecret  = "cert-secret"
	testIgnoreGlob  = "*.md"
	testMinimalName = "default-minimal"
	testProviderAWS = "aws"
)

func ptr[T any](v T) *T {
	return &v
}

func newRelease(namespace, name string, interval metav1.Duration) *nelmv1alpha1.Release {
	return &nelmv1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: nelmv1alpha1.ReleaseSpec{
			Interval: interval,
		},
	}
}

func TestBuildGitRepository(t *testing.T) { //nolint:gocyclo // table-driven field assertions
	relInterval := metav1.Duration{Duration: 5 * time.Minute}

	tests := []struct {
		name  string
		rel   *nelmv1alpha1.Release
		git   *nelmv1alpha1.GitRepositoryChartSource
		check func(t *testing.T, out *sourcev1.GitRepository)
	}{
		{
			name: "fully populated source maps every field",
			rel:  newRelease(testNS, "myrel", relInterval),
			git: &nelmv1alpha1.GitRepositoryChartSource{
				URL:                "https://github.com/example/repo.git",
				Branch:             "main",
				Tag:                "v1.0.0",
				SemVer:             ">=1.0.0",
				Commit:             "abc123",
				Reference:          "refs/heads/main",
				Path:               "charts/app",
				CredentialsFrom:    &nelmv1alpha1.CredentialReference{Kind: "Secret", Name: "git-secret"},
				ProxySettingsFrom:  &nelmv1alpha1.ProxySettingsReference{Kind: "Secret", Name: testProxySecret},
				Provider:           "github",
				ServiceAccountName: "git-sa",
				Submodules:         true,
				SparseCheckout:     []string{"charts/app"},
				Ignore:             ptr(testIgnoreGlob),
				Include: []sourcev1.GitRepositoryInclude{
					{GitRepositoryRef: meta.LocalObjectReference{Name: "dep"}},
				},
				Interval: metav1.Duration{Duration: 2 * time.Minute},
				Timeout:  &metav1.Duration{Duration: 90 * time.Second},
			},
			check: func(t *testing.T, out *sourcev1.GitRepository) {
				if out.APIVersion != sourcev1.GroupVersion.String() {
					t.Errorf("APIVersion = %q, want %q", out.APIVersion, sourcev1.GroupVersion.String())
				}
				if out.Kind != sourcev1.GitRepositoryKind {
					t.Errorf("Kind = %q, want %q", out.Kind, sourcev1.GitRepositoryKind)
				}
				if out.Name != testGitRelName {
					t.Errorf("Name = %q, want %q", out.Name, testGitRelName)
				}
				if out.Namespace != testNS {
					t.Errorf("Namespace = %q, want %q", out.Namespace, testNS)
				}
				if out.Spec.URL != "https://github.com/example/repo.git" {
					t.Errorf("URL = %q", out.Spec.URL)
				}
				if out.Spec.Reference == nil {
					t.Fatal("Reference is nil, want populated GitRepositoryRef")
				}
				if out.Spec.Reference.Branch != "main" {
					t.Errorf("Reference.Branch = %q", out.Spec.Reference.Branch)
				}
				if out.Spec.Reference.Tag != "v1.0.0" {
					t.Errorf("Reference.Tag = %q", out.Spec.Reference.Tag)
				}
				if out.Spec.Reference.SemVer != ">=1.0.0" {
					t.Errorf("Reference.SemVer = %q", out.Spec.Reference.SemVer)
				}
				if out.Spec.Reference.Commit != "abc123" {
					t.Errorf("Reference.Commit = %q", out.Spec.Reference.Commit)
				}
				// nelm Reference (json "ref") must land on FluxCD GitRepositoryRef.Name.
				if out.Spec.Reference.Name != "refs/heads/main" {
					t.Errorf("Reference.Name = %q, want %q", out.Spec.Reference.Name, "refs/heads/main")
				}
				if out.Spec.SecretRef == nil || out.Spec.SecretRef.Name != "git-secret" {
					t.Errorf("SecretRef = %+v, want name git-secret", out.Spec.SecretRef)
				}
				if out.Spec.ProxySecretRef == nil || out.Spec.ProxySecretRef.Name != testProxySecret {
					t.Errorf("ProxySecretRef = %+v, want name proxy-secret", out.Spec.ProxySecretRef)
				}
				if out.Spec.Provider != "github" {
					t.Errorf("Provider = %q, want github", out.Spec.Provider)
				}
				if out.Spec.ServiceAccountName != "git-sa" {
					t.Errorf("ServiceAccountName = %q, want git-sa", out.Spec.ServiceAccountName)
				}
				// nelm Submodules must map onto FluxCD RecurseSubmodules.
				if !out.Spec.RecurseSubmodules {
					t.Error("RecurseSubmodules = false, want true (from Submodules)")
				}
				if !reflect.DeepEqual(out.Spec.SparseCheckout, []string{"charts/app"}) {
					t.Errorf("SparseCheckout = %v", out.Spec.SparseCheckout)
				}
				if out.Spec.Ignore == nil || *out.Spec.Ignore != testIgnoreGlob {
					t.Errorf("Ignore = %v, want *.md", out.Spec.Ignore)
				}
				if len(out.Spec.Include) != 1 || out.Spec.Include[0].GitRepositoryRef.Name != "dep" {
					t.Errorf("Include = %+v, want one entry named dep", out.Spec.Include)
				}
				// Interval is set on the source, so it must NOT be overridden by the Release interval.
				if out.Spec.Interval != (metav1.Duration{Duration: 2 * time.Minute}) {
					t.Errorf("Interval = %v, want 2m", out.Spec.Interval)
				}
				if out.Spec.Timeout == nil || out.Spec.Timeout.Duration != 90*time.Second {
					t.Errorf("Timeout = %v, want 90s", out.Spec.Timeout)
				}
				// Guardrail: git commit verification must be dropped (type mismatch).
				if out.Spec.Verification != nil {
					t.Error("Verification should be nil (dropped due to type mismatch)")
				}
			},
		},
		{
			name: "minimal source inherits release interval and leaves optionals nil",
			rel:  newRelease("default", "minimal", relInterval),
			git: &nelmv1alpha1.GitRepositoryChartSource{
				URL:  "https://github.com/example/repo.git",
				Path: "chart",
			},
			check: func(t *testing.T, out *sourcev1.GitRepository) {
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
				if out.Spec.ProxySecretRef != nil {
					t.Errorf("ProxySecretRef = %+v, want nil", out.Spec.ProxySecretRef)
				}
				if out.Spec.Timeout != nil {
					t.Errorf("Timeout = %v, want nil", out.Spec.Timeout)
				}
				if out.Spec.Ignore != nil {
					t.Errorf("Ignore = %v, want nil", out.Spec.Ignore)
				}
				if out.Spec.RecurseSubmodules {
					t.Error("RecurseSubmodules = true, want false")
				}
				if out.Spec.Verification != nil {
					t.Error("Verification should be nil")
				}
			},
		},
		{
			name: "provider is passed through, not forced",
			rel:  newRelease(testNS, "passthrough", relInterval),
			git: &nelmv1alpha1.GitRepositoryChartSource{
				URL:      "https://github.com/example/repo.git",
				Path:     "chart",
				Provider: "generic",
			},
			check: func(t *testing.T, out *sourcev1.GitRepository) {
				if out.Spec.Provider != "generic" {
					t.Errorf("Provider = %q, want generic (passthrough)", out.Spec.Provider)
				}
				if out.Spec.Provider == testProviderAWS {
					t.Error("Provider must not be forced to aws")
				}
				if out.Spec.Verification != nil {
					t.Error("Verification should be nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := buildGitRepository(tt.rel, tt.git)
			tt.check(t, out)
		})
	}
}
