package source

import (
	"fmt"
	"strings"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func buildHelmRepository(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release, repo *nelmv1alpha1.HelmRepositoryChartSource) *sourcev1.HelmRepository {
	// OCI Helm registries must be declared with Type "oci"; everything else is
	// a classic index.yaml ("default") Helm repository.
	repoType := sourcev1.HelmRepositoryTypeDefault
	if strings.HasPrefix(repo.URL, "oci://") {
		repoType = sourcev1.HelmRepositoryTypeOCI
	}

	res := sourcev1.HelmRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourceAPIGroup + "/" + sourceAPIVersion,
			Kind:       sourcev1.HelmRepositoryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: double check on object naming
			Name:      fmt.Sprintf("%s-%s", rel.Namespace, "inline"),
			Namespace: rel.Namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL:             repo.URL,
			Type:            repoType,
			PassCredentials: repo.PassCredentials,
			Insecure:        repo.Insecure,
			Interval:        getNoneZeroDuration(repo.Interval, rel.Spec.Interval),
			Timeout:         repo.Timeout,
			// Provider is omitted: the nelm HelmRepositoryChartSource has no
			// Provider field, and the FluxCD CRD defaults it to "generic".
		},
	}

	if repo.CredentialsFrom != nil {
		res.Spec.SecretRef = &meta.LocalObjectReference{Name: repo.CredentialsFrom.Name}
	}

	if repo.CertificateFrom != nil {
		res.Spec.CertSecretRef = &meta.LocalObjectReference{Name: repo.CertificateFrom.Name}
	}

	return &res
}
