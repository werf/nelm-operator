package source

import (
	"context"
	"fmt"
	"os"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm/pkg/util"
)

// ChartResult is the outcome of resolving a chart source: a local path to the
// downloaded chart artifact and the source revision it was produced from.
type ChartResult struct {
	ChartPath   string
	Revision    string
	ValuesFiles []string
}

// InlineChartRef identifies the terminal FluxCD source object that carries the
// chart artifact for an inline (spec.chart) Release. For git/repo/bucket
// sources this is the companion HelmChart; for oci it is the OCIRepository
// itself. It is the contract handed from the ensure layer to the typed
// extractor.
type InlineChartRef struct {
	Kind      string
	Name      string
	Namespace string
}

// ResolveChartSource resolves the chart for a Release into a local artifact.
//
// For an inline chart (spec.chart) it ensures the FluxCD source objects exist
// (owned by the Release, applied server-side) and then extracts the artifact
// from the terminal object using the typed FluxCD API.
//
// For a chartRef (spec.chartRef) it reads a user-managed source object via the
// unstructured API (the kind/group/version are operator configuration) and
// extracts the artifact from it.
func ResolveChartSource(ctx context.Context, c client.Client, scheme *runtime.Scheme, rel *nelmv1alpha1.Release, sourceAPIGroup string, sourceAPIVersion string, tempDir string, httpRetry int, httpTimeout time.Duration) (*ChartResult, error) {
	if rel.Spec.Chart != nil {
		inlineRef, err := EnsureInlineChart(ctx, c, scheme, rel)
		if err != nil {
			return nil, fmt.Errorf("ensure inline chart source: %w", err)
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   sourceAPIGroup,
			Version: sourceAPIVersion,
			Kind:    inlineRef.Kind,
		})

		if err := c.Get(ctx, types.NamespacedName{Name: inlineRef.Name, Namespace: rel.Namespace}, obj); err != nil {
			return nil, fmt.Errorf("get source object %s/%s: %w", inlineRef.Kind, inlineRef.Name, err)
		}

		return extractArtifact(ctx, obj, tempDir, httpRetry, httpTimeout)
	}

	if rel.Spec.ChartRef != nil {
		return resolveChartRef(ctx, c, rel, sourceAPIGroup, sourceAPIVersion, tempDir, httpRetry, httpTimeout)
	}

	return nil, fmt.Errorf("neither spec.chart nor spec.chartRef is set")
}

// EnsureInlineChart materializes the FluxCD source objects for an inline chart
// and returns a reference to the terminal artifact-bearing object.
//
// Each branch builds the per-source object (and, for non-oci sources, the
// companion HelmChart) with the pure builders, sets the Release as controller
// owner, and applies it server-side. The oci branch deliberately creates no
// HelmChart: an OCIRepository is itself the chart artifact source.
func EnsureInlineChart(ctx context.Context, c client.Client, scheme *runtime.Scheme, rel *nelmv1alpha1.Release) (InlineChartRef, error) {
	chart := rel.Spec.Chart
	if chart == nil {
		return InlineChartRef{}, fmt.Errorf("spec.chart is not set")
	}

	name := fmt.Sprintf("%s-%s", rel.Namespace, rel.Name)
	helmChartRef := InlineChartRef{Kind: sourcev1.HelmChartKind, Name: name, Namespace: rel.Namespace}

	switch {
	case chart.GitRepositoryChartSource != nil:
		repo := buildGitRepository(rel, chart.GitRepositoryChartSource)
		if err := applyObject(ctx, c, scheme, rel, repo); err != nil {
			return InlineChartRef{}, fmt.Errorf("apply GitRepository: %w", err)
		}
		hc := buildHelmChartForGitRepositorySource(rel, chart.GitRepositoryChartSource)
		if err := applyObject(ctx, c, scheme, rel, hc); err != nil {
			return InlineChartRef{}, fmt.Errorf("apply HelmChart: %w", err)
		}
		return helmChartRef, nil

	case chart.HelmRepositoryChartSource != nil:
		repo := buildHelmRepository(rel, chart.HelmRepositoryChartSource)
		if err := applyObject(ctx, c, scheme, rel, repo); err != nil {
			return InlineChartRef{}, fmt.Errorf("apply HelmRepository: %w", err)
		}
		hc := buildHelmChartForHelmRepositorySource(rel, chart.HelmRepositoryChartSource)
		if err := applyObject(ctx, c, scheme, rel, hc); err != nil {
			return InlineChartRef{}, fmt.Errorf("apply HelmChart: %w", err)
		}
		return helmChartRef, nil

	case chart.BucketChartSource != nil:
		bucket := buildBucket(rel, chart.BucketChartSource)
		if err := applyObject(ctx, c, scheme, rel, bucket); err != nil {
			return InlineChartRef{}, fmt.Errorf("apply Bucket: %w", err)
		}
		hc := buildHelmChartForBucketSource(rel, chart.BucketChartSource)
		if err := applyObject(ctx, c, scheme, rel, hc); err != nil {
			return InlineChartRef{}, fmt.Errorf("apply HelmChart: %w", err)
		}
		return helmChartRef, nil

	case chart.OCIRepositoryChartSource != nil:
		// oci is terminal on its own; no companion HelmChart is created.
		oci := buildOCIRepository(rel, chart.OCIRepositoryChartSource)
		if err := applyObject(ctx, c, scheme, rel, oci); err != nil {
			return InlineChartRef{}, fmt.Errorf("apply OCIRepository: %w", err)
		}
		return InlineChartRef{Kind: sourcev1.OCIRepositoryKind, Name: name, Namespace: rel.Namespace}, nil

	default:
		return InlineChartRef{}, fmt.Errorf("no chart source configured: exactly one of git, repo, oci or bucket must be set")
	}
}

// applyObject sets the Release as the controlling owner of obj and applies it
// server-side. The typed builders populate obj's TypeMeta, which server-side
// apply requires.
func applyObject(ctx context.Context, c client.Client, scheme *runtime.Scheme, rel *nelmv1alpha1.Release, obj client.Object) error {
	if err := controllerutil.SetControllerReference(rel, obj, scheme); err != nil {
		return fmt.Errorf("set controller reference: %w", err)
	}

	// TODO: switch form deprecated mechanism
	if err := c.Patch(ctx, obj, client.Apply, client.FieldOwner("nelm-operator"), client.ForceOwnership); err != nil {
		return fmt.Errorf("apply %s %s/%s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err)
	}

	return nil
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

func extractArtifact(ctx context.Context, obj *unstructured.Unstructured, tempDir string, httpRetry int, httpTimeout time.Duration) (*ChartResult, error) {
	ready, msg, err := checkReadyCondition(obj)
	if err != nil {
		return nil, fmt.Errorf("check source readiness: %w", err)
	}
	if !ready {
		return nil, &SourceNotReadyError{Message: msg}
	}

	artifactURL, found, err := unstructured.NestedString(obj.Object, "status", "artifact", "url")
	if err != nil {
		return nil, fmt.Errorf("read .status.artifact.url: %w", err)
	}
	if !found || artifactURL == "" {
		return nil, fmt.Errorf("source object has no .status.artifact.url")
	}

	revision, _, _ := unstructured.NestedString(obj.Object, "status", "artifact", "revision")

	var valuesFiles []string

	if files, found, err := unstructured.NestedStringSlice(obj.Object, "spec", "valuesFiles"); err != nil {
		return nil, fmt.Errorf("read .spec.valuesFiles: %w", err)
	} else if found {
		valuesFiles = files
	}

	chartPath, err := downloadArtifact(ctx, artifactURL, tempDir, httpRetry, httpTimeout)
	if err != nil {
		return nil, fmt.Errorf("download chart artifact: %w", err)
	}

	return &ChartResult{
		ChartPath:   chartPath,
		Revision:    revision,
		ValuesFiles: valuesFiles,
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
		cond, ok := c.(map[string]any)
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

	restyClient := util.NewRestyClient(ctx).
		SetTimeout(timeout).
		SetRetryCount(maxRetries)

	resp, err := restyClient.R().
		SetContext(ctx).
		SetOutput(chartPath).
		Get(artifactURL)
	if err != nil {
		return "", fmt.Errorf("download artifact: %w", err)
	}

	if resp.IsError() {
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
