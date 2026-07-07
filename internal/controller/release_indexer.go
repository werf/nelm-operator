package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/fluxcd/pkg/runtime/conditions"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	indexSourceRef = ".metadata.sourceRef"
	indexConfigMap = ".metadata.configMap"
	indexSecret    = ".metadata.secret"
)

func isReadyOrReconciling(from conditions.Getter) bool {
	return conditions.IsReady(from) || conditions.IsReconciling(from)
}

func extractDigest(revision string) string {
	if strings.Contains(revision, "@") {
		// expects a revision in the <version>@<algorithm>:<digest> format
		tagDigestPair := strings.Split(revision, "@")
		if len(tagDigestPair) != 2 {
			return ""
		}
		return tagDigestPair[1]
	} else {
		// revision in the <algorithm>:<digest> format
		return revision
	}
}

func (r *ReleaseReconciler) requestsForHelmChartChange(ctx context.Context, o client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)

	hc, ok := o.(*sourcev1.HelmChart)
	if !ok {
		err := fmt.Errorf("expected a HelmChart, got %T", o)
		log.Error(err, "failed to get requests for HelmChart change")
		return nil
	}
	if hc.GetArtifact() == nil {
		return nil
	}

	var list nelmv1alpha1.ReleaseList
	if err := r.List(ctx, &list, client.MatchingFields{
		indexSourceRef: sourcev1.HelmChartKind + "/" + client.ObjectKeyFromObject(hc).String(),
	}); err != nil {
		log.Error(err, "failed to list Releases for HelmChart change")
		return nil
	}

	var reqs []reconcile.Request
	for i, hr := range list.Items {
		if isReadyOrReconciling(&list.Items[i]) && hc.GetArtifact().HasRevision(hr.Status.LastAttemptedArtifactRevision) {
			continue
		}
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}

	if len(reqs) > 0 {
		log.Info(
			"Enqueuing Releases for HelmChart change",
			"trigger", fmt.Sprintf("%s/%s", o.GetNamespace(), o.GetName()),
			"index", indexSourceRef,
			"releases", len(reqs),
		)
	}

	return reqs
}

func (r *ReleaseReconciler) requestsForOCIRepositoryChange(ctx context.Context, o client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)

	or, ok := o.(*sourcev1.OCIRepository)
	if !ok {
		err := fmt.Errorf("expected an OCIRepository, got %T", o)
		log.Error(err, "failed to get requests for OCIRepository change")
		return nil
	}

	if or.GetArtifact() == nil {
		return nil
	}

	var list nelmv1alpha1.ReleaseList
	if err := r.List(ctx, &list, client.MatchingFields{
		indexSourceRef: sourcev1.OCIRepositoryKind + "/" + client.ObjectKeyFromObject(or).String(),
	}); err != nil {
		log.Error(err, "failed to list Releases for OCIRepository change")
		return nil
	}

	var reqs []reconcile.Request
	for i, hr := range list.Items {
		digest := extractDigest(or.GetArtifact().Revision)
		if digest == "" {
			log.Error(fmt.Errorf("wrong digest for %T", or), "failed to get requests for OCIRepository change")
			continue
		}

		if isReadyOrReconciling(&list.Items[i]) && digest == hr.Status.LastAttemptedArtifactDigest {
			continue
		}

		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}

	if len(reqs) > 0 {
		log.Info(
			"Enqueuing Releases for OCIRepository change",
			"trigger", fmt.Sprintf("%s/%s", o.GetNamespace(), o.GetName()),
			"index", indexSourceRef,
			"releases", len(reqs),
		)
	}

	return reqs
}

func (r *ReleaseReconciler) requestsForConfigDependency(
	index string,
) func(ctx context.Context, o client.Object) []reconcile.Request {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		log := logf.FromContext(ctx)

		var list nelmv1alpha1.ReleaseList
		if err := r.List(ctx, &list, client.MatchingFields{
			index: client.ObjectKeyFromObject(o).String(),
		}); err != nil {
			log.Error(err, "failed to list Releases for config dependency change",
				"index", index, "objectRef", map[string]string{
					"name":      o.GetName(),
					"namespace": o.GetNamespace(),
				})
			return nil
		}

		reqs := make([]reconcile.Request, 0, len(list.Items))
		for i := range list.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&list.Items[i]),
			})
		}

		if len(reqs) > 0 {
			log.Info(
				"Enqueuing Releases for config dependency change",
				"trigger", fmt.Sprintf("%s/%s", o.GetNamespace(), o.GetName()),
				"index", index,
				"releases", len(reqs),
			)
		}

		return reqs
	}
}

func configDependencyIndexerFunc(kind string) client.IndexerFunc {
	return func(o client.Object) []string {
		obj := o.(*nelmv1alpha1.Release)
		namespace := obj.GetNamespace()
		var keys []string
		if val := obj.Spec.SecretKeyFrom; val != nil && val.Kind == kind {
			keys = append(keys, fmt.Sprintf("%s/%s", namespace, val.Name))
		}
		if val := obj.Spec.Provenance; val != nil && val.KeyringFrom != nil && val.KeyringFrom.Kind == kind {
			keys = append(keys, fmt.Sprintf("%s/%s", namespace, val.KeyringFrom.Name))
		}
		for _, ref := range obj.Spec.ValuesFrom {
			if ref.Kind == kind {
				keys = append(keys, fmt.Sprintf("%s/%s", namespace, ref.Name))
			}
		}
		for _, ref := range obj.Spec.SecretValuesFrom {
			if ref.Kind == kind {
				keys = append(keys, fmt.Sprintf("%s/%s", namespace, ref.Name))
			}
		}
		return keys
	}
}
