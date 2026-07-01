package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/werf/logboek"

	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/common"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm-operator/internal/config"
	"github.com/werf/nelm-operator/internal/source"
	"github.com/werf/nelm-operator/internal/values"
)

const finalizerName = "nelm.werf.io/release"

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
	Scheme *runtime.Scheme
	Config config.OperatorConfig
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

	if !controllerutil.ContainsFinalizer(&rel, finalizerName) {
		patch := client.MergeFrom(rel.DeepCopy())
		controllerutil.AddFinalizer(&rel, finalizerName)
		if err := r.Patch(ctx, &rel, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	return r.reconcileInstall(ctx, &rel)
}

func (r *ReleaseReconciler) reconcileInstall(ctx context.Context, rel *nelmv1alpha1.Release) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if rel.Status.ObservedGeneration != rel.Generation {
		rel.Status.LastActionFailures = 0
	}

	tempDir, err := os.MkdirTemp(r.Config.TempDir, "nelm-reconcile-*")
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	chartResult, err := source.ResolveChartSource(ctx, r.Client, r.Scheme, rel, r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, tempDir, r.Config.HTTPRetry, r.Config.HTTPTimeout)
	if err != nil {
		var notReady *source.SourceNotReadyError
		if errors.As(err, &notReady) {
			// FIXME: should be reflacted in conditions as well.
			log.Info("Source not ready, requeueing", "message", notReady.Message)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		return r.handleFailure(ctx, rel, false, fmt.Errorf("resolve chart source: %w", err))
	}

	resolvedValues, err := values.Resolve(ctx, r.Client, rel, chartResult.ValuesFiles, tempDir)
	if err != nil {
		return r.handleFailure(ctx, rel, false, fmt.Errorf("resolve values: %w", err))
	}

	releaseName := rel.Name
	releaseNamespace := rel.GetReleaseNamespace()

	planOpts := r.buildPlanInstallOptions(rel, chartResult.ChartPath, tempDir, resolvedValues)
	planArtifact, planErr := action.ReleasePlanInstall(ctx, releaseName, releaseNamespace, planOpts)

	if planErr == nil {
		log.Info("No changes detected, release is up to date")
		return r.handleSuccess(ctx, rel, releaseName, releaseNamespace)
	}

	if !errors.Is(planErr, action.ErrResourceChangesPlanned) && !errors.Is(planErr, action.ErrReleaseInstallPlanned) {
		return r.handleFailure(ctx, rel, false, fmt.Errorf("plan install: %w", planErr))
	}

	installOpts := r.buildInstallOptions(rel, chartResult.ChartPath, tempDir, resolvedValues)
	installOpts.LegacyPlanArtifact = planArtifact
	installOpts.PlanArtifactLifetime = 10 * time.Minute

	if err := action.ReleaseInstall(ctx, releaseName, releaseNamespace, installOpts); err != nil {
		return r.handleFailure(ctx, rel, true, fmt.Errorf("install release: %w", err))
	}

	rel.Status.LastAction = "install"
	return r.handleSuccess(ctx, rel, releaseName, releaseNamespace)
}

func (r *ReleaseReconciler) reconcileDelete(ctx context.Context, rel *nelmv1alpha1.Release) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(rel, finalizerName) {
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

	patch := client.MergeFrom(rel.DeepCopy())
	controllerutil.RemoveFinalizer(rel, finalizerName)
	if err := r.Patch(ctx, rel, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *ReleaseReconciler) handleSuccess(ctx context.Context, rel *nelmv1alpha1.Release, releaseName, releaseNamespace string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	historyResult, err := action.ReleaseHistory(ctx, releaseName, releaseNamespace, action.ReleaseHistoryOptions{
		KubeConnectionOptions:       r.buildKubeConnectionOptions(),
		OutputNoPrint:               true,
		ReleaseStorageDriver:        r.Config.ReleaseStorageDriver,
		ReleaseStorageSQLConnection: r.Config.ReleaseStorageSQLConnection,
		RevisionsLimit:              1,
	})
	if err != nil {
		log.Error(err, "Failed to fetch release history after successful install")
	} else if len(historyResult.Releases) == 0 {
		log.Error(nil, "No releases found in history after successful install")
	} else {
		latest := historyResult.Releases[len(historyResult.Releases)-1]
		rel.Status.Revision = latest.Revision
		rel.Status.RevisionStatus = string(latest.Status)
	}

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

func (r *ReleaseReconciler) handleFailure(ctx context.Context, rel *nelmv1alpha1.Release, installAttempted bool, reconcileErr error) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

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

	backoff := time.Duration(1<<rel.Status.LastActionFailures) * 15 * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}

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

		historyResult, historyErr := action.ReleaseHistory(ctx, releaseName, releaseNamespace, action.ReleaseHistoryOptions{
			KubeConnectionOptions:       r.buildKubeConnectionOptions(),
			OutputNoPrint:               true,
			ReleaseStorageDriver:        r.Config.ReleaseStorageDriver,
			ReleaseStorageSQLConnection: r.Config.ReleaseStorageSQLConnection,
			RevisionsLimit:              1,
		})
		if historyErr != nil {
			log.Error(historyErr, "Failed to fetch release history after rollback")
		} else if len(historyResult.Releases) == 0 {
			log.Error(nil, "No releases found in history after rollback")
		} else {
			latest := historyResult.Releases[len(historyResult.Releases)-1]
			rel.Status.Revision = latest.Revision
			rel.Status.RevisionStatus = string(latest.Status)
		}
	}
}

func (r *ReleaseReconciler) buildKubeConnectionOptions() common.KubeConnectionOptions {
	return common.KubeConnectionOptions{
		KubeQPSLimit:       r.Config.KubeQPSLimit,
		KubeBurstLimit:     r.Config.KubeBurstLimit,
		KubeRequestTimeout: r.Config.KubeRequestTimeout,
	}
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
		ReleaseLabels:               rel.Spec.ReleaseLabels,
		ReleaseStorageDriver:        r.Config.ReleaseStorageDriver,
		ReleaseStorageSQLConnection: r.Config.ReleaseStorageSQLConnection,
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
		KubeConnectionOptions:        r.buildKubeConnectionOptions(),
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
		KubeConnectionOptions:        r.buildKubeConnectionOptions(),
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
		KubeConnectionOptions:       r.buildKubeConnectionOptions(),
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
		ReleaseLabels:               rel.Spec.ReleaseLabels,
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
		KubeConnectionOptions:       r.buildKubeConnectionOptions(),
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
		For(&nelmv1alpha1.Release{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.Config.MaxConcurrentReconciles,
		}).
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
