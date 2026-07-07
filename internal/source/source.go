package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// Resolve chart from Release spec.chartRef.
func ResolveChartRef(ctx context.Context, c client.Client, sourceAPIGroup string, sourceAPIVersion string, chartRef *nelmv1alpha1.ChartSourceRef, tempDir string, httpRetry int, httpTimeout time.Duration) (*ChartResult, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   sourceAPIGroup,
		Version: sourceAPIVersion,
		Kind:    chartRef.Kind,
	})

	if err := c.Get(ctx, types.NamespacedName{Name: chartRef.Name, Namespace: chartRef.Namespace}, obj); err != nil {
		return nil, fmt.Errorf("get chart ref %s/%s: %w", chartRef.Kind, chartRef.Name, err)
	}

	return extractArtifact(ctx, obj, tempDir, httpRetry, httpTimeout)
}

func BuildChartSourceFromRelease(sourceAPIGroup string, sourceAPIVersion string, rel *nelmv1alpha1.Release) (client.Object, error) {
	chart := rel.Spec.Chart

	var source client.Object

	switch {
	case chart.GitRepositoryChartSource != nil:
		source = buildGitRepository(sourceAPIGroup, sourceAPIVersion, rel, chart.GitRepositoryChartSource)
		expectedSourceHashedName, err := GetObjectHashedName(source)
		if err != nil {
			return nil, fmt.Errorf("set chart source hashed name: %w", err)
		}
		source.SetName(expectedSourceHashedName)

	case chart.HelmRepositoryChartSource != nil:
		source = buildHelmRepository(sourceAPIGroup, sourceAPIVersion, rel, chart.HelmRepositoryChartSource)
		expectedSourceHashedName, err := GetObjectHashedName(source)
		if err != nil {
			return nil, fmt.Errorf("set chart source hashed name: %w", err)
		}
		source.SetName(expectedSourceHashedName)

	case chart.BucketChartSource != nil:
		source = buildBucket(sourceAPIGroup, sourceAPIVersion, rel, chart.BucketChartSource)
		expectedSourceHashedName, err := GetObjectHashedName(source)
		if err != nil {
			return nil, fmt.Errorf("set chart source hashed name: %w", err)
		}
		source.SetName(expectedSourceHashedName)

	case chart.OCIRepositoryChartSource != nil:
		// oci is terminal on its own; no companion HelmChart is created.
		source = buildOCIRepository(sourceAPIGroup, sourceAPIVersion, rel, chart.OCIRepositoryChartSource)
		expectedHashedName, err := GetObjectHashedName(source)
		if err != nil {
			return nil, fmt.Errorf("set chart source hashed name: %w", err)
		}
		source.SetName(expectedHashedName)

	default:
		return nil, fmt.Errorf("no chart source configured: exactly one of git, repo, oci or bucket must be set")
	}

	return source, nil
}

func GetObjectHashedName(obj client.Object) (string, error) {
	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return "", fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	spec, _ := unstructuredMap["spec"]

	specBytes, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal spec: %w", err)
	}

	hash := sha256.Sum256(specBytes)

	return fmt.Sprintf("%s-%s", strings.TrimRight(obj.GetName(), "-"), hex.EncodeToString(hash[:])[:12]), nil
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

	// TODO: verify ability to use custom values files embeded into chart artifact.
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
	defer func() {
		_ = f.Close()
	}()

	chartPath := f.Name()

	restyClient := util.NewRestyClient(ctx).
		SetTimeout(timeout).
		SetRetryCount(maxRetries)

	resp, err := restyClient.R().
		SetContext(ctx).
		SetOutput(chartPath).
		Get(artifactURL)
	if err != nil {
		_ = os.Remove(chartPath)
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
