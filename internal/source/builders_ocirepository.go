package source

import (
	"fmt"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

// buildOCIRepository maps a nelm OCIRepositoryChartSource onto a typed FluxCD
// OCIRepository object. It is a pure function: it has no side effects and does
// not set owner references (that is handled by the ensure* layer).
//
// Unlike a GitRepository or HelmRepository, an OCIRepository is itself the
// terminal artifact source for the chart, so no companion HelmChart is built.
func buildOCIRepository(rel *nelmv1alpha1.Release, oci *nelmv1alpha1.OCIRepositoryChartSource) *sourcev1.OCIRepository {
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

	res := sourcev1.OCIRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourcev1.GroupVersion.String(),
			Kind:       sourcev1.OCIRepositoryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: double check on object naming
			Name:      fmt.Sprintf("%s-%s", rel.Namespace, rel.Name),
			Namespace: rel.Namespace,
		},
		Spec: sourcev1.OCIRepositorySpec{
			URL:                oci.URL,
			Reference:          ref,
			Provider:           oci.Provider,
			ServiceAccountName: oci.ServiceAccountName,
			Insecure:           oci.Insecure,
			Ignore:             oci.Ignore,
			LayerSelector:      oci.LayerSelector,
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
