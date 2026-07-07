package source

import (
	"fmt"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func buildBucket(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release) *sourcev1.Bucket {
	bucket := rel.Spec.Chart.BucketChartSource

	res := sourcev1.Bucket{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourceAPIGroup + "/" + sourceAPIVersion,
			Kind:       sourcev1.BucketKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: double check on object naming
			Name:      fmt.Sprintf("%s-%s", rel.Namespace, "inline"),
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
