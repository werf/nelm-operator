package source

import (
	"fmt"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm/v2/pkg/helm/pkg/registry"
)

func buildOCIRepository(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release) *sourcev1.OCIRepository {
	oci := rel.Spec.Chart.OCIRepositoryChartSource

	// nelm exposes the OCI reference selectors (tag/semver/semverFilter/digest)
	// as flat fields, whereas FluxCD nests them in OCIRepositoryRef. Only build
	// the nested ref when at least one selector is set; otherwise leave it nil
	// so FluxCD defaults to the latest tag.
	var ref *sourcev1.OCIRepositoryRef
	if oci.Tag != "" || oci.SemVer != "" || oci.SemverFilter != "" || oci.Digest != "" {
		ref = &sourcev1.OCIRepositoryRef{
			Tag:          oci.Tag,
			SemVer:       oci.SemVer,
			SemverFilter: oci.SemverFilter,
			Digest:       oci.Digest,
		}
	}

	// A Release always needs a Helm chart, so pick its layer explicitly instead
	// of FluxCD's default of extracting whatever layer comes first. FluxCD
	// applies ignore only when extracting, so keep extracting if it is set.
	layerSelector := oci.LayerSelector
	if layerSelector == nil {
		layerSelector = &sourcev1.OCILayerSelector{
			MediaType: registry.ChartLayerMediaType,
			Operation: sourcev1.OCILayerCopy,
		}
		if oci.Ignore != nil {
			layerSelector.Operation = sourcev1.OCILayerExtract
		}
	}

	res := sourcev1.OCIRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourceAPIGroup + "/" + sourceAPIVersion,
			Kind:       sourcev1.OCIRepositoryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: double check on object naming
			Name:      fmt.Sprintf("%s-%s", rel.Namespace, "inline"),
			Namespace: rel.Namespace,
		},
		Spec: sourcev1.OCIRepositorySpec{
			URL:                oci.URL,
			Reference:          ref,
			Provider:           oci.Provider,
			ServiceAccountName: oci.ServiceAccountName,
			Insecure:           oci.Insecure,
			Ignore:             oci.Ignore,
			LayerSelector:      layerSelector,
			Interval:           getNoneZeroDuration(oci.Interval, rel.Spec.Interval),
			Timeout:            oci.Timeout,
			Verify:             oci.Verify,
		},
	}

	if oci.CredentialsFrom != nil {
		res.Spec.SecretRef = &meta.LocalObjectReference{Name: oci.CredentialsFrom.Name}
	}

	if oci.ProxySettingsFrom != nil {
		res.Spec.ProxySecretRef = &meta.LocalObjectReference{Name: oci.ProxySettingsFrom.Name}
	}

	if oci.CertificateFrom != nil {
		res.Spec.CertSecretRef = &meta.LocalObjectReference{Name: oci.CertificateFrom.Name}
	}

	// Unlike GitRepositoryVerification, the nelm and FluxCD OCI verification
	// types are identical (*sourcev1.OCIRepositoryVerification), so the value is
	// assigned directly rather than dropped.
	res.Spec.Verify = oci.Verify

	return &res
}
