package controller

import (
	"context"
	"fmt"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	intpredicates "github.com/werf/nelm-operator/internal/predicates"
	"github.com/werf/nelm-operator/internal/source"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func (r *ReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.TODO(), &nelmv1alpha1.Release{}, indexSourceRef,
		func(o client.Object) []string {
			obj := o.(*nelmv1alpha1.Release)
			var kind, name, namespace string
			switch {
			case obj.Spec.ChartRef != nil:
				kind = obj.Spec.ChartRef.Kind
				name = obj.Spec.ChartRef.Name
				// TODO: need to implement cross namespace acl to prevent references to other namespaces.
				namespace = obj.Spec.ChartRef.Namespace
				if namespace == "" {
					namespace = obj.GetNamespace()
				}
			case obj.Spec.Chart != nil && obj.Spec.Chart.OCIRepositoryChartSource != nil:
				repo, err := source.BuildChartSourceFromRelease(r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, obj)
				if err != nil {
					mgr.GetLogger().Error(err, "cannot index release source", "release", obj.GetName(), "namespace", obj.GetNamespace())
					return nil
				}
				kind = repo.GetObjectKind().GroupVersionKind().Kind
				name = repo.GetName()
				namespace = obj.GetNamespace()
			case obj.Spec.Chart != nil:
				kind = sourcev1.HelmChartKind
				name = source.GetHelmChartHashedName(obj.GetNamespace(), obj.GetName())
				namespace = obj.GetNamespace()
			default:
				return nil
			}
			return []string{fmt.Sprintf("%s/%s/%s", kind, namespace, name)}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.TODO(), &nelmv1alpha1.Release{}, indexConfigMap, configDependencyIndexerFunc("ConfigMap"),
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.TODO(), &nelmv1alpha1.Release{}, indexSecret, configDependencyIndexerFunc("Secret"),
	); err != nil {
		return err
	}

	var configDependencyPredicate predicate.Predicate = predicate.NewPredicateFuncs(func(o client.Object) bool {
		return r.Config.DependencyWatchLabelSelector.Matches(labels.Set(o.GetLabels()))
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&nelmv1alpha1.Release{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.Config.MaxConcurrentReconciles,
		}).
		Watches(
			&sourcev1.HelmChart{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForHelmChartChange),
			builder.WithPredicates(intpredicates.SourceRevisionChangePredicate{}),
		).
		Watches(
			&sourcev1.OCIRepository{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForOCIRepositoryChange),
			builder.WithPredicates(intpredicates.SourceRevisionChangePredicate{}),
		).
		WatchesMetadata(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForConfigDependency(indexConfigMap)),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, configDependencyPredicate),
		).
		WatchesMetadata(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForConfigDependency(indexSecret)),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, configDependencyPredicate),
		).
		Named("release").
		Complete(r)
}
