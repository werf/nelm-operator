package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/werf/logboek"

	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/common"
	"github.com/werf/nelm/pkg/kube"
	"github.com/werf/nelm/pkg/release"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm-operator/internal/config"
	"github.com/werf/nelm-operator/internal/source"
	"github.com/werf/nelm-operator/internal/utils"
	"github.com/werf/nelm-operator/internal/values"
)

const (
	ownershipMarkerKey   = "nelm.werf.io/owned-by"
	ownershipMarkerValue = "nelm-operator"

	reasonForeignChangeAdopted     = "ForeignChangeAdopted"
	reasonForeignChangeReconciled  = "ForeignChangeReconciled"
	reasonForeignUninstallRepaired = "ForeignUninstallRepaired"
)

// +kubebuilder:rbac:groups=nelm.werf.io,resources=releases,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=nelm.werf.io,resources=releases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nelm.werf.io,resources=releases/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;create;delete
// +kubebuilder:rbac:groups="*",resources="*",verbs="*"

type ReleaseReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Config        config.OperatorConfig
	EventRecorder record.EventRecorder
}

func (r *ReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = logboek.NewContext(ctx, logboek.DefaultLogger())
	log := logf.FromContext(ctx)

	var rel nelmv1alpha1.Release
	if err := r.Get(ctx, req.NamespacedName, &rel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !rel.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &rel)
	}

	if rel.Spec.Suspend {
		log.V(1).Info("Reconciliation suspended, skipping")
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&rel, nelmv1alpha1.ReleaseFinalizerName) {
		patch := client.MergeFrom(rel.DeepCopy())
		controllerutil.AddFinalizer(&rel, nelmv1alpha1.ReleaseFinalizerName)
		if err := r.Patch(ctx, &rel, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	var chartRef *nelmv1alpha1.ChartSourceRef

	if rel.Spec.Chart != nil {
		expectedChartSource, err := source.BuildChartSourceFromRelease(r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, &rel)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("build source repo from inline spec: %w", err)
		}

		expectedChartSourceRef := &nelmv1alpha1.ChartSourceReference{
			Group:     r.Config.SourceAPIGroup,
			Version:   r.Config.SourceAPIVersion,
			Kind:      expectedChartSource.GetObjectKind().GroupVersionKind().Kind,
			Namespace: expectedChartSource.GetNamespace(),
			Name:      expectedChartSource.GetName(),
		}

		switch rel.Status.ChartSourcePhase {
		case "", nelmv1alpha1.ChartSourcePhaseReady:
			if !reflect.DeepEqual(rel.Status.LastAppliedChartSource, expectedChartSourceRef) {
				// TODO: make sense to mark Reconcile condition as True here.
				rel.Status.ChartSourcePhase = nelmv1alpha1.ChartSourcePhasePending
				rel.Status.CandidateChartSource = expectedChartSourceRef
				if err := r.Status().Update(ctx, &rel); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}
		case nelmv1alpha1.ChartSourcePhasePending:
			// TODO: make sense to mark Reconcile condition as True here.
			if !reflect.DeepEqual(rel.Status.CandidateChartSource, expectedChartSourceRef) {
				rel.Status.CandidateChartSource = expectedChartSourceRef
				if err := r.Status().Update(ctx, &rel); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.ensureChartSourceRef(ctx, &rel, expectedChartSource); err != nil {
				return ctrl.Result{}, fmt.Errorf("ensure inline source: %w", err)
			}

			rel.Status.ChartSourcePhase = nelmv1alpha1.ChartSourcePhaseDisconnecting
			if err := r.Status().Update(ctx, &rel); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil

		case nelmv1alpha1.ChartSourcePhaseDisconnecting:
			// TODO: make sense to mark Reconcile condition as True here.
			if rel.Status.LastAppliedChartSource != nil && !reflect.DeepEqual(rel.Status.LastAppliedChartSource, rel.Status.CandidateChartSource) {
				if err := r.cleanupChartSourceRef(ctx, &rel, rel.Status.LastAppliedChartSource); err != nil {
					return ctrl.Result{}, err
				}
			}

			rel.Status.LastAppliedChartSource = rel.Status.CandidateChartSource.DeepCopy()
			rel.Status.ChartSourcePhase = nelmv1alpha1.ChartSourcePhaseReady
			if err := r.Status().Update(ctx, &rel); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		chartRef, err = r.ensureHelmChart(ctx, &rel, expectedChartSource.GetName())
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure chart source HelmChart: %w", err)
		}
	} else {
		// Cleanup chartSource on switching from spec.Chart to spec.chartRef.
		if rel.Status.LastAppliedChartSource != nil || rel.Status.CandidateChartSource != nil {
			if err := r.cleanupChartSourceRef(ctx, &rel, rel.Status.LastAppliedChartSource); err != nil {
				return ctrl.Result{}, fmt.Errorf("cleanup former chart source ref: %w", err)
			}
			if err := r.cleanupChartSourceRef(ctx, &rel, rel.Status.CandidateChartSource); err != nil {
				return ctrl.Result{}, fmt.Errorf("cleanup former chart source ref: %w", err)
			}
			rel.Status.LastAppliedChartSource = nil
			rel.Status.ChartSourcePhase = ""
			rel.Status.CandidateChartSource = nil
			if err := r.Status().Update(ctx, &rel); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.cleanupHelmChart(ctx, &rel); err != nil {
				return ctrl.Result{}, fmt.Errorf("cleanup former helm chart: %w", err)
			}
			return ctrl.Result{Requeue: true}, nil
		}

		chartRef = rel.Spec.ChartRef
		if chartRef.Namespace == "" {
			chartRef.Namespace = rel.Namespace
		}
	}

	releaseName := rel.Name
	releaseNamespace := rel.GetReleaseNamespace()

	preRel, err := r.getRelease(ctx, &rel, releaseName, releaseNamespace)
	if err != nil {
		return r.handleFailure(ctx, &rel, false, fmt.Errorf("read release state: %w", err), nil, false)
	}
	foreignChange := detectForeignChange(rel.Status.Revision, preRel)

	tempDir, err := os.MkdirTemp(r.Config.TempDir, "nelm-reconcile-*")
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	chartResult, err := source.ResolveChartRef(ctx, r.Client, r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, chartRef, tempDir, r.Config.HTTPRetry, r.Config.HTTPTimeout)
	if err != nil {
		var notReady *source.SourceNotReadyError
		if errors.As(err, &notReady) {
			// FIXME: should be reflacted in conditions as well.
			log.Info("Source not ready, requeueing", "message", notReady.Message)
			r.projectStatusFromStorage(ctx, &rel, releaseName, releaseNamespace, preRel, false)
			if err := r.Status().Update(ctx, &rel); err != nil {
				return ctrl.Result{}, fmt.Errorf("update source-not-ready status: %w", err)
			}
			// TODO: remove RequeueAfter when indexer based HelmChart be implemented. Currently this hack required for .spec.chartRef.
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		return r.handleFailure(ctx, &rel, false, fmt.Errorf("resolve chart source: %w", err), preRel, false)
	}

	return r.reconcileInstall(ctx, &rel, chartResult, tempDir, preRel, foreignChange)
}

func (r *ReleaseReconciler) ensureChartSourceRef(ctx context.Context, rel *nelmv1alpha1.Release, chartSource client.Object) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		unstruct := &unstructured.Unstructured{}
		unstruct.SetGroupVersionKind(
			schema.GroupVersionKind{
				Group:   r.Config.SourceAPIGroup,
				Version: r.Config.SourceAPIVersion,
				Kind:    chartSource.GetObjectKind().GroupVersionKind().Kind,
			},
		)
		err := r.Get(ctx, client.ObjectKey{Namespace: chartSource.GetNamespace(), Name: chartSource.GetName()}, unstruct)
		if apierrors.IsNotFound(err) {
			annotations := chartSource.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[nelmv1alpha1.SourceRefReleaseRefAnnotationName] = rel.Name
			chartSource.SetAnnotations(annotations)
			return r.Create(ctx, chartSource)
		} else if err != nil {
			return fmt.Errorf("get source %s/%s/%s: %w", chartSource.GetNamespace(), chartSource.GetObjectKind().GroupVersionKind().Kind, chartSource.GetName(), err)
		}

		annotations := unstruct.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		releaseNames := getChartSourceReleaseRefNamesSlice(unstruct)
		if !slices.Contains(releaseNames, rel.Name) {
			releaseNames = append(releaseNames, rel.Name)
		}

		annotations[nelmv1alpha1.SourceRefReleaseRefAnnotationName] = strings.Join(releaseNames, ",")
		unstruct.SetAnnotations(annotations)

		return r.Update(ctx, unstruct)
	})
}

func (r *ReleaseReconciler) cleanupChartSourceRef(ctx context.Context, rel *nelmv1alpha1.Release, chartSourceRef *nelmv1alpha1.ChartSourceReference) error {
	if chartSourceRef == nil {
		return nil
	}

	var latestSource client.Object
	var latestReleaseNames []string

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		unstruct := &unstructured.Unstructured{}
		unstruct.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   r.Config.SourceAPIGroup,
			Version: r.Config.SourceAPIVersion,
			Kind:    chartSourceRef.Kind,
		})
		if err := r.Get(ctx, client.ObjectKey{Namespace: chartSourceRef.Namespace, Name: chartSourceRef.Name}, unstruct); err != nil {
			return client.IgnoreNotFound(err)
		}

		releaseNames := getChartSourceReleaseRefNamesSlice(unstruct)
		if !slices.Contains(releaseNames, rel.Name) {
			return nil
		} else {
			index := slices.Index(releaseNames, rel.Name)
			if index != -1 {
				releaseNames = slices.Delete(releaseNames, index, index+1)
			}
		}

		annotations := unstruct.GetAnnotations()

		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[nelmv1alpha1.SourceRefReleaseRefAnnotationName] = strings.Join(releaseNames, ",")
		unstruct.SetAnnotations(annotations)

		if err := r.Update(ctx, unstruct); err != nil {
			return err
		}

		latestSource = unstruct
		latestReleaseNames = releaseNames

		return nil
	})

	if err != nil || latestSource == nil {
		return err
	}

	if len(latestReleaseNames) == 0 {
		resourceVersion, UID := latestSource.GetResourceVersion(), latestSource.GetUID()
		deleteOptions := &client.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				ResourceVersion: &resourceVersion,
				UID:             &UID,
			},
		}
		err := r.Delete(ctx, latestSource, deleteOptions)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *ReleaseReconciler) ensureHelmChart(ctx context.Context, rel *nelmv1alpha1.Release, sourceName string) (*nelmv1alpha1.ChartSourceRef, error) {
	chart := rel.Spec.Chart
	if chart == nil {
		return nil, fmt.Errorf("spec.chart is not set")
	}

	var obj client.Object

	switch {
	case chart.GitRepositoryChartSource != nil:
		obj = source.BuildHelmChartForGitRepositorySource(r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, rel, sourceName, chart.GitRepositoryChartSource)
	case chart.HelmRepositoryChartSource != nil:
		obj = source.BuildHelmChartForHelmRepositorySource(r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, rel, sourceName, chart.HelmRepositoryChartSource)
	case chart.BucketChartSource != nil:
		obj = source.BuildHelmChartForBucketSource(r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, rel, sourceName, chart.BucketChartSource)
	case chart.OCIRepositoryChartSource != nil:
		return &nelmv1alpha1.ChartSourceRef{
			APIVersion: r.Config.SourceAPIGroup + "/" + r.Config.SourceAPIVersion,
			Kind:       sourcev1.OCIRepositoryKind,
			Name:       sourceName,
			Namespace:  rel.Namespace,
		}, nil
	default:
		return nil, fmt.Errorf("no chart source configured: exactly one of git, repo, oci or bucket must be set")
	}

	if err := controllerutil.SetControllerReference(rel, obj, r.Scheme); err != nil {
		return nil, fmt.Errorf("set helm chart controller reference: %w", err)
	}

	if err := r.Patch(ctx, obj, client.Apply, client.FieldOwner("nelm-operator"), client.ForceOwnership); err != nil {
		return nil, fmt.Errorf("apply helm chart %s %s/%s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err)
	}

	return &nelmv1alpha1.ChartSourceRef{
		APIVersion: r.Config.SourceAPIGroup + "/" + r.Config.SourceAPIVersion,
		Kind:       sourcev1.HelmChartKind,
		Namespace:  rel.Namespace,
		Name:       obj.GetName(),
	}, nil
}

func (r *ReleaseReconciler) cleanupHelmChart(ctx context.Context, rel *nelmv1alpha1.Release) error {
	unstruct := &unstructured.Unstructured{}
	unstruct.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   r.Config.SourceAPIGroup,
		Version: r.Config.SourceAPIVersion,
		Kind:    sourcev1.HelmChartKind,
	})

	helmChartName := source.GetHelmChartHashedName(rel.Namespace, rel.Name)
	unstruct.SetName(helmChartName)
	unstruct.SetNamespace(rel.Namespace)

	err := r.Delete(ctx, unstruct)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete HelmChart %s/%s: %w", rel.Namespace, helmChartName, err)
	}
	return nil
}

func getChartSourceReleaseRefNamesSlice(obj client.Object) []string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return []string{}
	}
	val, _ := annotations[nelmv1alpha1.SourceRefReleaseRefAnnotationName]
	return strings.Split(val, ",")
}

func (r *ReleaseReconciler) reconcileInstall(ctx context.Context, rel *nelmv1alpha1.Release, chartResult *source.ChartResult, tempDir string, preRel *action.ReleaseGetResultRelease, foreignChange bool) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if rel.Status.ObservedGeneration != rel.Generation {
		rel.Status.LastActionFailures = 0
	}

	releaseName := rel.Name
	releaseNamespace := rel.GetReleaseNamespace()

	resolvedValues, err := values.Resolve(ctx, r.Client, rel, chartResult.ValuesFiles, tempDir)
	if err != nil {
		return r.handleFailure(ctx, rel, false, fmt.Errorf("resolve values: %w", err), preRel, false)
	}

	planOpts := r.buildPlanInstallOptions(rel, chartResult.ChartPath, tempDir, resolvedValues)
	planArtifact, planErr := action.ReleasePlanInstall(ctx, releaseName, releaseNamespace, planOpts)

	if planErr == nil {
		if preRel != nil && preRel.StorageLabels[ownershipMarkerKey] != ownershipMarkerValue {
			if err := r.stampOwnershipMarker(ctx, rel, releaseName, releaseNamespace, preRel.Revision); err != nil {
				return r.handleFailure(ctx, rel, false, fmt.Errorf("stamp ownership marker: %w", err), preRel, false)
			}
		}
		if foreignChange && preRel != nil {
			r.emitEvent(rel, corev1.EventTypeNormal, reasonForeignChangeAdopted,
				fmt.Sprintf("Adopted foreign release revision %d matching desired state", preRel.Revision))
		}
		log.Info("No changes detected, release is up to date")
		return r.handleSuccess(ctx, rel, releaseName, releaseNamespace, preRel, false)
	}

	if !errors.Is(planErr, action.ErrResourceChangesPlanned) && !errors.Is(planErr, action.ErrReleaseInstallPlanned) {
		return r.handleFailure(ctx, rel, false, fmt.Errorf("plan install: %w", planErr), preRel, false)
	}

	installOpts := r.buildInstallOptions(rel, chartResult.ChartPath, tempDir, resolvedValues)
	installOpts.LegacyPlanArtifact = planArtifact
	installOpts.PlanArtifactLifetime = 10 * time.Minute

	if err := action.ReleaseInstall(ctx, releaseName, releaseNamespace, installOpts); err != nil {
		return r.handleFailure(ctx, rel, true, fmt.Errorf("install release: %w", err), nil, true)
	}

	if foreignChange {
		if preRel != nil {
			r.emitEvent(rel, corev1.EventTypeWarning, reasonForeignChangeReconciled,
				"Reconciled foreign release change back to desired state")
		} else {
			r.emitEvent(rel, corev1.EventTypeWarning, reasonForeignUninstallRepaired,
				"Reinstalled release after foreign uninstall")
		}
	}

	rel.Status.LastAction = "install"
	return r.handleSuccess(ctx, rel, releaseName, releaseNamespace, nil, true)
}

// getRelease reads the latest stored release revision. A missing release is
// reported as (nil, nil) rather than an error so callers can treat absence as
// a first-time deploy or a foreign uninstall.
func (r *ReleaseReconciler) getRelease(ctx context.Context, rel *nelmv1alpha1.Release, releaseName, releaseNamespace string) (*action.ReleaseGetResultRelease, error) {
	result, err := action.ReleaseGet(ctx, releaseName, releaseNamespace, action.ReleaseGetOptions{
		KubeConnectionOptions:       r.buildKubeConnectionOptions(rel),
		OutputNoPrint:               true,
		ReleaseStorageDriver:        r.Config.ReleaseStorageDriver,
		ReleaseStorageSQLConnection: r.Config.ReleaseStorageSQLConnection,
		Revision:                    0,
	})
	if err != nil {
		var notFound *action.ReleaseNotFoundError
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, err
	}
	return result.Release, nil
}

// detectForeignChange reports whether the current storage state diverged from
// what the operator last recorded. A release with no recorded revision is a
// first-time adoption, never a foreign change. A higher storage revision or a
// latest revision missing the ownership marker (helm does not carry custom
// labels onto a new revision) both indicate an out-of-band change.
func detectForeignChange(recordedRevision int, rel *action.ReleaseGetResultRelease) bool {
	if recordedRevision == 0 {
		return false
	}
	if rel == nil {
		return true
	}
	if rel.Revision > recordedRevision {
		return true
	}
	return rel.StorageLabels[ownershipMarkerKey] != ownershipMarkerValue
}

// stampOwnershipMarker merges the ownership marker into the release storage
// labels via nelm, without creating a new revision. It works across all
// storage drivers (secret, configmap, sql, memory).
func (r *ReleaseReconciler) stampOwnershipMarker(ctx context.Context, rel *nelmv1alpha1.Release, releaseName, releaseNamespace string, revision int) error {
	log := logf.FromContext(ctx)

	kubeConfig, err := kube.NewKubeConfig(ctx, kube.KubeConfigOptions{
		KubeConnectionOptions: r.buildKubeConnectionOptions(rel),
		KubeContextNamespace:  releaseNamespace,
	})
	if err != nil {
		return fmt.Errorf("construct kube config: %w", err)
	}

	clientFactory, err := kube.NewClientFactory(ctx, kubeConfig)
	if err != nil {
		return fmt.Errorf("construct kube client factory: %w", err)
	}

	releaseStorage, err := release.NewReleaseStorage(ctx, releaseNamespace, r.Config.ReleaseStorageDriver, clientFactory, release.ReleaseStorageOptions{
		SQLConnection: r.Config.ReleaseStorageSQLConnection,
	})
	if err != nil {
		return fmt.Errorf("construct release storage: %w", err)
	}

	if err := releaseStorage.UpdateLabels(releaseName, revision, map[string]string{ownershipMarkerKey: ownershipMarkerValue}); err != nil {
		return fmt.Errorf("update release labels: %w", err)
	}

	log.Info("Stamped ownership marker on adopted release", "revision", revision)
	return nil
}

// projectStatusFromStorage recomputes release-derived status fields from the
// current stored release. When refetch is false the caller-supplied release is
// used as-is (nil means the release is absent, so the fields are cleared);
// when true a fresh read is issued and, on read error, prior fields are left
// untouched rather than failing the reconcile.
func (r *ReleaseReconciler) projectStatusFromStorage(ctx context.Context, rel *nelmv1alpha1.Release, releaseName, releaseNamespace string, captured *action.ReleaseGetResultRelease, refetch bool) {
	log := logf.FromContext(ctx)

	current := captured
	if refetch {
		var err error
		current, err = r.getRelease(ctx, rel, releaseName, releaseNamespace)
		if err != nil {
			log.Error(err, "Failed to project status from release storage")
			return
		}
	}

	if current == nil {
		rel.Status.Revision = 0
		rel.Status.RevisionStatus = ""
		return
	}

	rel.Status.Revision = current.Revision
	rel.Status.RevisionStatus = string(current.Status)
}

func (r *ReleaseReconciler) emitEvent(rel *nelmv1alpha1.Release, eventtype, reason, message string) {
	if r.EventRecorder == nil {
		return
	}
	r.EventRecorder.Event(rel, eventtype, reason, message)
}

func (r *ReleaseReconciler) reconcileDelete(ctx context.Context, rel *nelmv1alpha1.Release) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(rel, nelmv1alpha1.ReleaseFinalizerName) {
		return ctrl.Result{}, nil
	}

	r.setCondition(rel, metav1.Condition{
		Type:               "Reconciling",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: rel.Generation,
		Reason:             "Uninstalling",
		Message:            "Uninstalling release",
	})
	rel.Status.ObservedGeneration = rel.Generation
	if err := r.Status().Update(ctx, rel); err != nil {
		return ctrl.Result{}, fmt.Errorf("update uninstalling status: %w", err)
	}

	tempDir, err := os.MkdirTemp(r.Config.TempDir, "nelm-uninstall-*")
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	uninstallOpts := r.buildUninstallOptions(rel, tempDir)
	releaseName := rel.Name
	releaseNamespace := rel.GetReleaseNamespace()

	if err := action.ReleaseUninstall(ctx, releaseName, releaseNamespace, uninstallOpts); err != nil {
		return ctrl.Result{}, fmt.Errorf("uninstall release: %w", err)
	}

	rel.Status.LastAction = "uninstall"
	if err := r.Status().Update(ctx, rel); err != nil {
		return ctrl.Result{}, fmt.Errorf("update uninstall status: %w", err)
	}

	if rel.Status.LastAppliedChartSource != nil {
		if err := r.cleanupChartSourceRef(ctx, rel, rel.Status.LastAppliedChartSource); err != nil {
			return ctrl.Result{}, fmt.Errorf("delete inline source ref on delete reconcile: %w", err)
		}
	}

	patch := client.MergeFrom(rel.DeepCopy())
	controllerutil.RemoveFinalizer(rel, nelmv1alpha1.ReleaseFinalizerName)
	if err := r.Patch(ctx, rel, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *ReleaseReconciler) handleSuccess(ctx context.Context, rel *nelmv1alpha1.Release, releaseName, releaseNamespace string, captured *action.ReleaseGetResultRelease, refetch bool) (ctrl.Result, error) {
	r.projectStatusFromStorage(ctx, rel, releaseName, releaseNamespace, captured, refetch)

	rel.Status.LastActionFailures = 0
	rel.Status.ObservedGeneration = rel.Generation

	r.setCondition(rel, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: rel.Generation,
		Reason:             "InstallSucceeded",
		Message:            fmt.Sprintf("Release install complete, revision %d", rel.Status.Revision),
	})
	r.setCondition(rel, metav1.Condition{
		Type:               "Reconciling",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: rel.Generation,
		Reason:             "Succeeded",
	})
	r.setCondition(rel, metav1.Condition{
		Type:               "Stalled",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: rel.Generation,
		Reason:             "Succeeded",
	})

	if err := r.Status().Update(ctx, rel); err != nil {
		return ctrl.Result{}, fmt.Errorf("update success status: %w", err)
	}

	return ctrl.Result{RequeueAfter: rel.Spec.Interval.Duration}, nil
}

func (r *ReleaseReconciler) handleFailure(ctx context.Context, rel *nelmv1alpha1.Release, installAttempted bool, reconcileErr error, captured *action.ReleaseGetResultRelease, refetch bool) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	r.projectStatusFromStorage(ctx, rel, rel.Name, rel.GetReleaseNamespace(), captured, refetch)

	rel.Status.LastActionFailures++
	rel.Status.ObservedGeneration = rel.Generation

	maxRetries := 0
	if rel.Spec.Install != nil {
		maxRetries = rel.Spec.Install.Retries
	}

	if rel.Status.LastActionFailures > maxRetries {
		if installAttempted && rel.Spec.Install != nil && rel.Spec.Install.AutoRollback {
			log.Info("Retries exhausted, attempting auto-rollback")
			r.attemptRollback(ctx, rel)
		}

		r.setCondition(rel, metav1.Condition{
			Type:               "Stalled",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: rel.Generation,
			Reason:             "RetriesExhausted",
			Message:            reconcileErr.Error(),
		})
		r.setCondition(rel, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: rel.Generation,
			Reason:             "InstallFailed",
			Message:            reconcileErr.Error(),
		})
		r.setCondition(rel, metav1.Condition{
			Type:               "Reconciling",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: rel.Generation,
			Reason:             "RetriesExhausted",
		})

		if err := r.Status().Update(ctx, rel); err != nil {
			return ctrl.Result{}, fmt.Errorf("update stalled status: %w", err)
		}

		return ctrl.Result{}, nil
	}

	r.setCondition(rel, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: rel.Generation,
		Reason:             "InstallFailed",
		Message:            reconcileErr.Error(),
	})
	r.setCondition(rel, metav1.Condition{
		Type:               "Reconciling",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: rel.Generation,
		Reason:             "RetryPending",
		Message:            fmt.Sprintf("Retry %d of %d", rel.Status.LastActionFailures, maxRetries),
	})
	r.setCondition(rel, metav1.Condition{
		Type:               "Stalled",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: rel.Generation,
		Reason:             "RetryPending",
	})

	if err := r.Status().Update(ctx, rel); err != nil {
		return ctrl.Result{}, fmt.Errorf("update failure status: %w", err)
	}

	backoff := min(time.Duration(1<<rel.Status.LastActionFailures)*15*time.Second, 5*time.Minute)

	return ctrl.Result{RequeueAfter: backoff}, nil
}

func (r *ReleaseReconciler) attemptRollback(ctx context.Context, rel *nelmv1alpha1.Release) {
	log := logf.FromContext(ctx)

	tempDir, err := os.MkdirTemp(r.Config.TempDir, "nelm-rollback-*")
	if err != nil {
		log.Error(err, "Failed to create temp dir for rollback")
		return
	}
	defer os.RemoveAll(tempDir)

	rollbackOpts := r.buildRollbackOptions(rel, tempDir)
	releaseName := rel.Name
	releaseNamespace := rel.GetReleaseNamespace()

	if err := action.ReleaseRollback(ctx, releaseName, releaseNamespace, rollbackOpts); err != nil {
		log.Error(err, "Auto-rollback failed")
	} else {
		log.Info("Auto-rollback succeeded")
		rel.Status.LastAction = "rollback"
		r.projectStatusFromStorage(ctx, rel, releaseName, releaseNamespace, nil, true)
	}
}

func (r *ReleaseReconciler) buildKubeConnectionOptions(rel *nelmv1alpha1.Release) common.KubeConnectionOptions {
	const (
		inClusterCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		inClusterTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	)

	apiServerHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	apiServerPort := os.Getenv("KUBERNETES_SERVICE_PORT")
	if apiServerPort == "" {
		apiServerPort = "443"
	}

	options := common.KubeConnectionOptions{
		KubeAPIServerAddress: "https://" + net.JoinHostPort(apiServerHost, apiServerPort),
		KubeTLSCAPath:        inClusterCAPath,
		KubeBearerTokenPath:  inClusterTokenPath,
		KubeQPSLimit:         r.Config.KubeQPSLimit,
		KubeBurstLimit:       r.Config.KubeBurstLimit,
		KubeRequestTimeout:   r.Config.KubeRequestTimeout,
	}

	var serviceAccount string

	if r.Config.DefaultServiceAccountName != "" {
		serviceAccount = r.Config.DefaultServiceAccountName
	}

	if rel.Spec.ServiceAccountName != "" {
		serviceAccount = rel.Spec.ServiceAccountName
	}

	if serviceAccount != "" {
		user := fmt.Sprintf("system:serviceaccount:%s:%s", rel.GetReleaseNamespace(), serviceAccount)
		options.KubeImpersonateUser = user
	}

	return options
}

func (r *ReleaseReconciler) buildTrackingOptions(rel *nelmv1alpha1.Release) common.TrackingOptions {
	opts := common.TrackingOptions{
		NoProgressTablePrint: true,
		NoPodLogs:            true,
	}

	if rel.Spec.Tracking != nil {
		opts.TrackReadinessTimeout = rel.Spec.Tracking.ReadinessTimeout.Duration
		opts.TrackCreationTimeout = rel.Spec.Tracking.CreationTimeout.Duration
		opts.TrackDeletionTimeout = rel.Spec.Tracking.DeletionTimeout.Duration
		opts.NoPodLogs = !rel.Spec.Tracking.PodLogs
		opts.NoFinalTracking = !rel.Spec.Tracking.FinalTracking
	}

	return opts
}

func (r *ReleaseReconciler) buildValidationOptions(rel *nelmv1alpha1.Release) common.ResourceValidationOptions {
	opts := common.ResourceValidationOptions{
		ValidationKubeVersion:         common.DefaultResourceValidationKubeVersion,
		ValidationSchemas:             common.DefaultResourceValidationSchema,
		ValidationSchemaCacheLifetime: common.DefaultResourceValidationCacheLifetime,
	}

	if rel.Spec.Validation == nil {
		return opts
	}

	v := rel.Spec.Validation
	opts.NoResourceValidation = v.NoResourceValidation
	opts.NoValuesSchemaValidation = v.NoValuesSchemaValidation
	opts.LocalResourceValidation = v.LocalOnly
	if v.KubeVersion != "" {
		opts.ValidationKubeVersion = v.KubeVersion
	}
	opts.ValidationSkip = v.Skip
	opts.ValidationSchemas = v.Schemas
	opts.ValidationExtraSchemas = v.ExtraSchemas
	opts.ValidationSchemaCacheLifetime = v.SchemaCacheLifetime.Duration

	return opts
}

func (r *ReleaseReconciler) buildRuntimeOptions(rel *nelmv1alpha1.Release) common.ReleaseInstallRuntimeOptions {
	opts := common.ReleaseInstallRuntimeOptions{
		ResourceValidationOptions:   r.buildValidationOptions(rel),
		ExtraAnnotations:            rel.Spec.ExtraAnnotations,
		ExtraLabels:                 rel.Spec.ExtraLabels,
		ExtraRuntimeAnnotations:     rel.Spec.RuntimeAnnotations,
		ExtraRuntimeLabels:          rel.Spec.RuntimeLabels,
		ReleaseInfoAnnotations:      rel.Spec.ReleaseInfoAnnotations,
		ReleaseLabels:               ownershipLabels(rel.Spec.ReleaseLabels),
		ReleaseStorageDriver:        r.Config.ReleaseStorageDriver,
		ReleaseStorageSQLConnection: r.Config.ReleaseStorageSQLConnection,
		ForceAdoption:               true,
	}

	if rel.Spec.ReleaseStorage != nil {
		opts.ReleaseHistoryLimit = rel.Spec.ReleaseStorage.HistoryLimit
	}

	if rel.Spec.Install != nil {
		install := rel.Spec.Install
		opts.NoInstallStandaloneCRDs = install.NoInstallCRDs
		opts.DefaultDeletePropagation = install.DeletePropagation
		opts.ForceAdoption = !install.NoForceAdoption
		opts.NoRemoveManualChanges = install.NoRemoveManualChanges
	}

	return opts
}

// ownershipLabels returns a fresh label map carrying the user-supplied release
// labels with the ownership marker forced on, without mutating the input map.
func ownershipLabels(userLabels map[string]string) map[string]string {
	merged := make(map[string]string, len(userLabels)+1)
	maps.Copy(merged, userLabels)
	merged[ownershipMarkerKey] = ownershipMarkerValue
	return merged
}

func (r *ReleaseReconciler) buildValuesOptions(rel *nelmv1alpha1.Release, resolvedValues *values.ResolvedValues) common.ValuesOptions {
	opts := common.ValuesOptions{
		ValuesFiles: resolvedValues.ValuesFiles,
		RootSetJSON: rel.Spec.SetRootContextJSON,
	}

	opts.DefaultValuesDisable = rel.Spec.NoDefaultValues

	return opts
}

func (r *ReleaseReconciler) buildSecretValuesOptions(rel *nelmv1alpha1.Release, resolvedValues *values.ResolvedValues) common.SecretValuesOptions {
	opts := common.SecretValuesOptions{
		SecretKey:         resolvedValues.SecretKey,
		SecretValuesFiles: resolvedValues.SecretValuesFiles,
	}

	opts.DefaultSecretValuesDisable = rel.Spec.NoDefaultSecretValues

	return opts
}

func (r *ReleaseReconciler) buildPlanInstallOptions(rel *nelmv1alpha1.Release, chartPath string, tempDir string, resolvedValues *values.ResolvedValues) action.ReleasePlanInstallOptions {
	return action.ReleasePlanInstallOptions{
		KubeConnectionOptions:        r.buildKubeConnectionOptions(rel),
		ReleaseInstallRuntimeOptions: r.buildRuntimeOptions(rel),
		ValuesOptions:                r.buildValuesOptions(rel, resolvedValues),
		SecretValuesOptions:          r.buildSecretValuesOptions(rel, resolvedValues),

		Chart:                   chartPath,
		ChartAppVersion:         rel.Spec.AppVersion,
		ChartProvenanceKeyring:  resolvedValues.ProvenanceKeyringPath,
		ChartProvenanceStrategy: provenanceStrategy(rel),
		DenoBinaryPath:          r.Config.DenoBinaryPath,
		ErrorIfChangesPlanned:   true,
		IgnoreBundleJS:          typescriptIgnoreBundleJS(rel),
		NetworkParallelism:      r.Config.NetworkParallelism,
		TemplatesAllowDNS:       installTemplatesAllowDNS(rel),
		TempDirPath:             tempDir,
		Timeout:                 rel.GetInstallTimeout(),
	}
}

func (r *ReleaseReconciler) buildInstallOptions(rel *nelmv1alpha1.Release, chartPath string, tempDir string, resolvedValues *values.ResolvedValues) action.ReleaseInstallOptions {
	return action.ReleaseInstallOptions{
		KubeConnectionOptions:        r.buildKubeConnectionOptions(rel),
		ReleaseInstallRuntimeOptions: r.buildRuntimeOptions(rel),
		TrackingOptions:              r.buildTrackingOptions(rel),
		ValuesOptions:                r.buildValuesOptions(rel, resolvedValues),
		SecretValuesOptions:          r.buildSecretValuesOptions(rel, resolvedValues),

		Chart:                   chartPath,
		ChartAppVersion:         rel.Spec.AppVersion,
		ChartProvenanceKeyring:  resolvedValues.ProvenanceKeyringPath,
		ChartProvenanceStrategy: provenanceStrategy(rel),
		DenoBinaryPath:          r.Config.DenoBinaryPath,
		IgnoreBundleJS:          typescriptIgnoreBundleJS(rel),
		NetworkParallelism:      r.Config.NetworkParallelism,
		TemplatesAllowDNS:       installTemplatesAllowDNS(rel),
		TempDirPath:             tempDir,
		Timeout:                 rel.GetInstallTimeout(),
	}
}

func (r *ReleaseReconciler) buildRollbackOptions(rel *nelmv1alpha1.Release, tempDir string) action.ReleaseRollbackOptions {
	opts := action.ReleaseRollbackOptions{
		KubeConnectionOptions:       r.buildKubeConnectionOptions(rel),
		ResourceValidationOptions:   r.buildValidationOptions(rel),
		TrackingOptions:             r.buildTrackingOptions(rel),
		ReleaseStorageDriver:        r.Config.ReleaseStorageDriver,
		ReleaseStorageSQLConnection: r.Config.ReleaseStorageSQLConnection,
		NetworkParallelism:          r.Config.NetworkParallelism,
		TempDirPath:                 tempDir,
		Timeout:                     rel.GetRollbackTimeout(),
		ExtraRuntimeAnnotations:     rel.Spec.RuntimeAnnotations,
		ExtraRuntimeLabels:          rel.Spec.RuntimeLabels,
		ReleaseInfoAnnotations:      rel.Spec.ReleaseInfoAnnotations,
		ReleaseLabels:               ownershipLabels(rel.Spec.ReleaseLabels),
		ForceAdoption:               true,
		Revision:                    0,
	}

	if rel.Spec.ReleaseStorage != nil {
		opts.ReleaseHistoryLimit = rel.Spec.ReleaseStorage.HistoryLimit
	}

	if rel.Spec.Rollback != nil {
		rb := rel.Spec.Rollback
		opts.DefaultDeletePropagation = rb.DeletePropagation
		opts.ForceAdoption = !rb.NoForceAdoption
		opts.NoRemoveManualChanges = rb.NoRemoveManualChanges
	}

	return opts
}

func (r *ReleaseReconciler) buildUninstallOptions(rel *nelmv1alpha1.Release, tempDir string) action.ReleaseUninstallOptions {
	opts := action.ReleaseUninstallOptions{
		KubeConnectionOptions:       r.buildKubeConnectionOptions(rel),
		TrackingOptions:             r.buildTrackingOptions(rel),
		ReleaseStorageDriver:        r.Config.ReleaseStorageDriver,
		ReleaseStorageSQLConnection: r.Config.ReleaseStorageSQLConnection,
		NetworkParallelism:          r.Config.NetworkParallelism,
		TempDirPath:                 tempDir,
		Timeout:                     rel.GetUninstallTimeout(),
	}

	if rel.Spec.ReleaseStorage != nil {
		opts.ReleaseHistoryLimit = rel.Spec.ReleaseStorage.HistoryLimit
	}

	if rel.Spec.Uninstall != nil {
		un := rel.Spec.Uninstall
		opts.DeleteReleaseNamespace = un.DeleteNamespace
		opts.DefaultDeletePropagation = un.DeletePropagation
		opts.NoRemoveManualChanges = un.NoRemoveManualChanges
	}

	return opts
}

func (r *ReleaseReconciler) setCondition(rel *nelmv1alpha1.Release, condition metav1.Condition) {
	meta.SetStatusCondition(&rel.Status.Conditions, condition)
}

func (r *ReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&nelmv1alpha1.Release{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.Config.MaxConcurrentReconciles,
		}).
		// TODO: switch to more optimized soulution based on idnexers. It also solves tracking of non-inline charts
		Watches(
			&sourcev1.HelmChart{},
			handler.EnqueueRequestsFromMapFunc(
				utils.MapInternalResources(
					nelmv1alpha1.HelmChartReleaseRefLabelName,
				),
			),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldChart, ok1 := e.ObjectOld.(*sourcev1.HelmChart)
					newChart, ok2 := e.ObjectNew.(*sourcev1.HelmChart)
					if ok1 && ok2 {
						if oldChart.Status.Artifact != nil && newChart.Status.Artifact != nil {
							if oldChart.Status.Artifact.Digest != newChart.Status.Artifact.Digest {
								return true
							}
						}
						return !reflect.DeepEqual(oldChart.Status, newChart.Status)
					}
					return false
				},
			}),
		).
		// TODO: add watchers for related secrets and configmaps.
		Named("release").
		Complete(r)
}

func provenanceStrategy(rel *nelmv1alpha1.Release) string {
	if rel.Spec.Provenance != nil {
		return rel.Spec.Provenance.Strategy
	}
	return ""
}

func installTemplatesAllowDNS(rel *nelmv1alpha1.Release) bool {
	if rel.Spec.Install != nil {
		return rel.Spec.Install.TemplatesAllowDNS
	}
	return false
}

func typescriptIgnoreBundleJS(rel *nelmv1alpha1.Release) bool {
	if rel.Spec.Typescript != nil {
		return rel.Spec.Typescript.IgnoreBundleJS
	}
	return false
}
