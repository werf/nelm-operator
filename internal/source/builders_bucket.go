package source

import (
	"fmt"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

// buildBucket maps a nelm BucketChartSource onto a typed FluxCD Bucket object.
// It is a pure function: it has no side effects and does not set owner
// references (that is handled by the ensure* layer).
//
// Note: bucket.Path and bucket.ValuesFiles are intentionally NOT mapped here.
// They look like Bucket fields, but FluxCD models the chart path and values on
// the HelmChart (spec.Chart), not on the Bucket source. Setting them on a
// BucketSpec would not even compile.
func buildBucket(rel *nelmv1alpha1.Release, bucket *nelmv1alpha1.BucketChartSource) *sourcev1.Bucket {
	res := sourcev1.Bucket{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourcev1.GroupVersion.String(),
			Kind:       sourcev1.BucketKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: double check on object naming
			Name:      fmt.Sprintf("%s-%s", rel.Namespace, rel.Name),
			Namespace: rel.Namespace,
		},
		Spec: sourcev1.BucketSpec{
			Endpoint:           bucket.Endpoint,
			BucketName:         bucket.BucketName,
			Provider:           bucket.Provider,
			Region:             bucket.Region,
			Prefix:             bucket.Prefix,
			ServiceAccountName: bucket.ServiceAccountName,
			Insecure:           bucket.Insecure,
			Ignore:             bucket.Ignore,
			STS:                bucket.STS,
			Interval:           getNoneZeroDuration(bucket.Interval, rel.Spec.Interval),
			Timeout:            bucket.Timeout,
		},
	}

	if bucket.CredentialsFrom != nil {
		res.Spec.SecretRef = &meta.LocalObjectReference{Name: bucket.CredentialsFrom.Name}
	}

	if bucket.CertificateFrom != nil {
		res.Spec.CertSecretRef = &meta.LocalObjectReference{Name: bucket.CertificateFrom.Name}
	}

	if bucket.ProxySettingsFrom != nil {
		res.Spec.ProxySecretRef = &meta.LocalObjectReference{Name: bucket.ProxySettingsFrom.Name}
	}

	return &res
}
