package source

import (
	"testing"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func TestBuildBucket(t *testing.T) { //nolint:gocyclo // table-driven field assertions
	relInterval := metav1.Duration{Duration: 5 * time.Minute}

	tests := []struct {
		name   string
		rel    *nelmv1alpha1.Release
		bucket *nelmv1alpha1.BucketChartSource
		check  func(t *testing.T, out *sourcev1.Bucket)
	}{
		{
			name: "fully populated source maps core Bucket spec fields",
			rel:  newRelease(testNS, "myrel", relInterval),
			bucket: &nelmv1alpha1.BucketChartSource{
				Endpoint:   "s3.example.com",
				BucketName: "charts",
				// Path and ValuesFiles are HelmChart concerns and must never leak
				// onto the Bucket. They are set here purely to prove that.
				Path:        "charts/app",
				ValuesFiles: []string{"values-prod.yaml"},
				Provider:    testProviderAWS,
				Region:      "us-east-1",
				Prefix:      "releases/",
				Insecure:    true,
				Ignore:      ptr(testIgnoreGlob),
				STS: &sourcev1.BucketSTSSpec{
					Provider: testProviderAWS,
					Endpoint: "https://sts.example.com",
				},
				ServiceAccountName: "bucket-sa",
				Interval:           metav1.Duration{Duration: 2 * time.Minute},
				Timeout:            &metav1.Duration{Duration: 90 * time.Second},
			},
			check: func(t *testing.T, out *sourcev1.Bucket) {
				if out.APIVersion != sourcev1.GroupVersion.String() {
					t.Errorf("APIVersion = %q, want %q", out.APIVersion, sourcev1.GroupVersion.String())
				}
				if out.Kind != sourcev1.BucketKind {
					t.Errorf("Kind = %q, want %q", out.Kind, sourcev1.BucketKind)
				}
				if out.Spec.Endpoint != "s3.example.com" {
					t.Errorf("Endpoint = %q, want s3.example.com", out.Spec.Endpoint)
				}
				if out.Spec.BucketName != "charts" {
					t.Errorf("BucketName = %q, want charts", out.Spec.BucketName)
				}
				if out.Spec.Provider != testProviderAWS {
					t.Errorf("Provider = %q, want aws", out.Spec.Provider)
				}
				if out.Spec.Region != "us-east-1" {
					t.Errorf("Region = %q, want us-east-1", out.Spec.Region)
				}
				if out.Spec.Prefix != "releases/" {
					t.Errorf("Prefix = %q, want releases/", out.Spec.Prefix)
				}
				if out.Spec.STS == nil {
					t.Fatal("STS is nil, want populated BucketSTSSpec")
				}
				if out.Spec.STS.Provider != testProviderAWS || out.Spec.STS.Endpoint != "https://sts.example.com" {
					t.Errorf("STS = %+v, want provider aws / endpoint https://sts.example.com", out.Spec.STS)
				}
				if !out.Spec.Insecure {
					t.Error("Insecure = false, want true")
				}
				if out.Spec.ServiceAccountName != "bucket-sa" {
					t.Errorf("ServiceAccountName = %q, want bucket-sa", out.Spec.ServiceAccountName)
				}
				if out.Spec.Ignore == nil || *out.Spec.Ignore != testIgnoreGlob {
					t.Errorf("Ignore = %v, want *.md", out.Spec.Ignore)
				}
				// Source-level interval is explicitly set, so it must be kept and
				// NOT overridden by the Release interval.
				if out.Spec.Interval != (metav1.Duration{Duration: 2 * time.Minute}) {
					t.Errorf("Interval = %v, want 2m (source value kept)", out.Spec.Interval)
				}
				if out.Spec.Timeout == nil || out.Spec.Timeout.Duration != 90*time.Second {
					t.Errorf("Timeout = %v, want 90s", out.Spec.Timeout)
				}
			},
		},
		{
			name: "name and namespace are derived from the release",
			rel:  newRelease("team-x", "rel-y", relInterval),
			bucket: &nelmv1alpha1.BucketChartSource{
				Endpoint:   "s3.example.com",
				BucketName: "charts",
				Path:       "chart",
			},
			check: func(t *testing.T, out *sourcev1.Bucket) {
				if out.Name != "team-x-rel-y" {
					t.Errorf("Name = %q, want team-x-rel-y", out.Name)
				}
				if out.Namespace != "team-x" {
					t.Errorf("Namespace = %q, want team-x", out.Namespace)
				}
			},
		},
		{
			name: "zero source interval inherits the release interval",
			rel:  newRelease("default", "minimal", relInterval),
			bucket: &nelmv1alpha1.BucketChartSource{
				Endpoint:   "s3.example.com",
				BucketName: "charts",
				Path:       "chart",
				// Interval left zero on purpose.
			},
			check: func(t *testing.T, out *sourcev1.Bucket) {
				if out.Spec.Interval != relInterval {
					t.Errorf("Interval = %v, want injected release interval %v", out.Spec.Interval, relInterval)
				}
			},
		},
		{
			name: "secret, cert and proxy refs pass through unchanged",
			rel:  newRelease(testNS, "refs", relInterval),
			bucket: &nelmv1alpha1.BucketChartSource{
				Endpoint:          "s3.example.com",
				BucketName:        "charts",
				Path:              "chart",
				CredentialsFrom:   &nelmv1alpha1.CredentialReference{Kind: "Secret", Name: "bucket-secret"},
				CertificateFrom:   &nelmv1alpha1.CertificateReference{Name: testCertSecret},
				ProxySettingsFrom: &nelmv1alpha1.ProxySettingsReference{Kind: "Secret", Name: testProxySecret},
			},
			check: func(t *testing.T, out *sourcev1.Bucket) {
				if out.Spec.SecretRef == nil || out.Spec.SecretRef.Name != "bucket-secret" {
					t.Errorf("SecretRef = %+v, want name bucket-secret", out.Spec.SecretRef)
				}
				if out.Spec.CertSecretRef == nil || out.Spec.CertSecretRef.Name != testCertSecret {
					t.Errorf("CertSecretRef = %+v, want name cert-secret", out.Spec.CertSecretRef)
				}
				if out.Spec.ProxySecretRef == nil || out.Spec.ProxySecretRef.Name != testProxySecret {
					t.Errorf("ProxySecretRef = %+v, want name proxy-secret", out.Spec.ProxySecretRef)
				}
			},
		},
		{
			name: "omitted optional refs stay nil (nil-safe)",
			rel:  newRelease("default", "bare", relInterval),
			bucket: &nelmv1alpha1.BucketChartSource{
				Endpoint:   "s3.example.com",
				BucketName: "charts",
				Path:       "chart",
			},
			check: func(t *testing.T, out *sourcev1.Bucket) {
				if out.Spec.SecretRef != nil {
					t.Errorf("SecretRef = %+v, want nil", out.Spec.SecretRef)
				}
				if out.Spec.CertSecretRef != nil {
					t.Errorf("CertSecretRef = %+v, want nil", out.Spec.CertSecretRef)
				}
				if out.Spec.ProxySecretRef != nil {
					t.Errorf("ProxySecretRef = %+v, want nil", out.Spec.ProxySecretRef)
				}
				if out.Spec.STS != nil {
					t.Errorf("STS = %+v, want nil", out.Spec.STS)
				}
				if out.Spec.Timeout != nil {
					t.Errorf("Timeout = %v, want nil", out.Spec.Timeout)
				}
				if out.Spec.Ignore != nil {
					t.Errorf("Ignore = %v, want nil", out.Spec.Ignore)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := buildBucket(tt.rel, tt.bucket)
			tt.check(t, out)
		})
	}
}
