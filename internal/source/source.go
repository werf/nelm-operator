package source

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm/pkg/util"
)

type ChartResult struct {
	ChartPath string
	Revision  string
}

func ResolveChartSource(ctx context.Context, c client.Client, rel *nelmv1alpha1.Release, sourceAPIGroup string, sourceAPIVersion string, tempDir string, httpRetry int, httpTimeout time.Duration) (*ChartResult, error) {
	if rel.Spec.Chart != nil {
		return resolveInlineChart(ctx, c, rel, sourceAPIGroup, sourceAPIVersion, tempDir, httpRetry, httpTimeout)
	}

	if rel.Spec.ChartRef != nil {
		return resolveChartRef(ctx, c, rel, sourceAPIGroup, sourceAPIVersion, tempDir, httpRetry, httpTimeout)
	}

	return nil, fmt.Errorf("neither spec.chart nor spec.chartRef is set")
}

func resolveInlineChart(ctx context.Context, c client.Client, rel *nelmv1alpha1.Release, sourceAPIGroup string, sourceAPIVersion string, tempDir string, httpRetry int, httpTimeout time.Duration) (*ChartResult, error) {
	helmChart, err := ensureHelmChart(ctx, c, rel, sourceAPIGroup, sourceAPIVersion)
	if err != nil {
		return nil, fmt.Errorf("ensure HelmChart object: %w", err)
	}

	return extractArtifact(ctx, helmChart, tempDir, httpRetry, httpTimeout)
}

func resolveChartRef(ctx context.Context, c client.Client, rel *nelmv1alpha1.Release, sourceAPIGroup string, sourceAPIVersion string, tempDir string, httpRetry int, httpTimeout time.Duration) (*ChartResult, error) {
	ref := rel.Spec.ChartRef

	ns := ref.Namespace
	if ns == "" {
		ns = rel.Namespace
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   sourceAPIGroup,
		Version: sourceAPIVersion,
		Kind:    ref.Kind,
	})

	if err := c.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, obj); err != nil {
		return nil, fmt.Errorf("get source object %s/%s: %w", ref.Kind, ref.Name, err)
	}

	return extractArtifact(ctx, obj, tempDir, httpRetry, httpTimeout)
}

func ensureHelmChart(ctx context.Context, c client.Client, rel *nelmv1alpha1.Release, sourceAPIGroup string, sourceAPIVersion string) (*unstructured.Unstructured, error) {
	chartSpec := rel.Spec.Chart.Spec
	chartName := fmt.Sprintf("%s-%s", rel.Namespace, rel.Name)

	gvk := schema.GroupVersionKind{
		Group:   sourceAPIGroup,
		Version: sourceAPIVersion,
		Kind:    "HelmChart",
	}

	desired := buildHelmChartObject(rel, chartSpec, chartName, gvk, sourceAPIGroup)

	if err := c.Patch(ctx, desired, client.Apply, client.FieldOwner("nelm-operator"), client.ForceOwnership); err != nil {
		return nil, fmt.Errorf("apply HelmChart: %w", err)
	}

	return desired, nil
}

func buildHelmChartObject(rel *nelmv1alpha1.Release, chartSpec nelmv1alpha1.ReleaseChartSpec, name string, gvk schema.GroupVersionKind, sourceAPIGroup string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	obj.SetNamespace(rel.Namespace)
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         nelmv1alpha1.SchemeGroupVersion.String(),
			Kind:               "Release",
			Name:               rel.Name,
			UID:                rel.UID,
			Controller:         lo.ToPtr(true),
			BlockOwnerDeletion: lo.ToPtr(true),
		},
	})

	sourceRef := map[string]interface{}{
		"kind": chartSpec.SourceRef.Kind,
		"name": chartSpec.SourceRef.Name,
	}
	if chartSpec.SourceRef.Namespace != "" {
		sourceRef["namespace"] = chartSpec.SourceRef.Namespace
	}

	spec := map[string]interface{}{
		"chart":     chartSpec.Chart,
		"interval":  chartSpec.Interval.Duration.String(),
		"sourceRef": sourceRef,
	}

	if chartSpec.Version != "" {
		spec["version"] = chartSpec.Version
	}

	if chartSpec.ReconcileStrategy != "" {
		spec["reconcileStrategy"] = chartSpec.ReconcileStrategy
	}

	if chartSpec.Verify != nil {
		verify := map[string]interface{}{
			"provider": chartSpec.Verify.Provider,
		}
		if chartSpec.Verify.SecretRef != nil {
			verify["secretRef"] = map[string]interface{}{
				"name": chartSpec.Verify.SecretRef.Name,
			}
		}
		spec["verify"] = verify
	}

	if err := unstructured.SetNestedField(obj.Object, spec, "spec"); err != nil {
		panic(fmt.Sprintf("set HelmChart spec: %v", err))
	}

	return obj
}

func extractArtifact(ctx context.Context, obj *unstructured.Unstructured, tempDir string, httpRetry int, httpTimeout time.Duration) (*ChartResult, error) {
	ready, msg, err := checkReadyCondition(obj)
	if err != nil {
		return nil, fmt.Errorf("check source readiness: %w", err)
	}
	if !ready {
		return nil, &SourceNotReadyError{Message: msg}
	}

	artifactURL, found, err := unstructured.NestedString(obj.Object, "status", "artifact", "url")
	if err != nil || !found || artifactURL == "" {
		return nil, fmt.Errorf("source object has no .status.artifact.url")
	}

	revision, _, _ := unstructured.NestedString(obj.Object, "status", "artifact", "revision")

	chartPath, err := downloadArtifact(ctx, artifactURL, tempDir, httpRetry, httpTimeout)
	if err != nil {
		return nil, fmt.Errorf("download chart artifact: %w", err)
	}

	return &ChartResult{
		ChartPath: chartPath,
		Revision:  revision,
	}, nil
}

func checkReadyCondition(obj *unstructured.Unstructured) (bool, string, error) {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return false, "", fmt.Errorf("read conditions: %w", err)
	}
	if !found {
		return false, "no conditions found", nil
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(cond, "type")
		if condType != "Ready" {
			continue
		}

		status, _, _ := unstructured.NestedString(cond, "status")
		message, _, _ := unstructured.NestedString(cond, "message")
		return status == "True", message, nil
	}

	return false, "Ready condition not found", nil
}

func downloadArtifact(ctx context.Context, artifactURL string, tempDir string, maxRetries int, timeout time.Duration) (string, error) {
	f, err := os.CreateTemp(tempDir, "chart-*.tgz")
	if err != nil {
		return "", fmt.Errorf("create chart temp file: %w", err)
	}
	chartPath := f.Name()
	f.Close()

	restyClient := util.NewRestyClient(ctx).
		SetTimeout(timeout).
		SetRetryCount(maxRetries)

	resp, err := restyClient.R().
		SetContext(ctx).
		SetOutput(chartPath).
		Get(artifactURL)
	if err != nil {
		os.Remove(chartPath)
		return "", fmt.Errorf("download artifact: %w", err)
	}

	if resp.IsError() {
		os.Remove(chartPath)
		return "", fmt.Errorf("download artifact: unexpected status %d", resp.StatusCode())
	}

	return chartPath, nil
}

type SourceNotReadyError struct {
	Message string
}

func (e *SourceNotReadyError) Error() string {
	return fmt.Sprintf("source not ready: %s", e.Message)
}
