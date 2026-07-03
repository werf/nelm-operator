package source

import (
	"fmt"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func buildGitRepository(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release, git *nelmv1alpha1.GitRepositoryChartSource) *sourcev1.GitRepository {
	res := sourcev1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: sourceAPIGroup + "/" + sourceAPIVersion,
			Kind:       sourcev1.GitRepositoryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: double check on object naming
			Name:      fmt.Sprintf("%s-%s", rel.Namespace, "inline"),
			Namespace: rel.Namespace,
		},
		Spec: sourcev1.GitRepositorySpec{
			URL: git.URL,
			Reference: &sourcev1.GitRepositoryRef{
				Branch: git.Branch,
				Tag:    git.Tag,
				SemVer: git.SemVer,
				Commit: git.Commit,
				// nelm Reference (json "ref") maps to FluxCD GitRepositoryRef.Name.
				Name: git.Reference,
			},
			Provider:           git.Provider,
			ServiceAccountName: git.ServiceAccountName,
			RecurseSubmodules:  git.Submodules,
			SparseCheckout:     git.SparseCheckout,
			Ignore:             git.Ignore,
			Include:            git.Include,
			Interval:           getNoneZeroDuration(git.Interval, rel.Spec.Interval),
			Timeout:            git.Timeout,
			Verification:       git.Verification,
		},
	}

	if git.CredentialsFrom != nil {
		res.Spec.SecretRef = &meta.LocalObjectReference{Name: git.CredentialsFrom.Name}
	}

	if git.ProxySettingsFrom != nil {
		res.Spec.ProxySecretRef = &meta.LocalObjectReference{Name: git.ProxySettingsFrom.Name}
	}

	return &res
}
