package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm/v2/pkg/common"
	"github.com/werf/nelm/v2/pkg/helm/pkg/chart/loader/archive"
	chartutil "github.com/werf/nelm/v2/pkg/helm/pkg/chart/v2/util"
	"github.com/werf/nelm/v2/pkg/util"
)

// ChartResult is the outcome of resolving a chart source: a local path to the
// downloaded chart artifact and the source revision it was produced from.
type ChartResult struct {
	ChartPath string
	Digest    string
	Revision  string
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
		source = buildGitRepository(sourceAPIGroup, sourceAPIVersion, rel)
		expectedSourceHashedName, err := GetObjectHashedName(source)
		if err != nil {
			return nil, fmt.Errorf("set chart source hashed name: %w", err)
		}
		source.SetName(expectedSourceHashedName)

	case chart.HelmRepositoryChartSource != nil:
		source = buildHelmRepository(sourceAPIGroup, sourceAPIVersion, rel)
		expectedSourceHashedName, err := GetObjectHashedName(source)
		if err != nil {
			return nil, fmt.Errorf("set chart source hashed name: %w", err)
		}
		source.SetName(expectedSourceHashedName)

	case chart.BucketChartSource != nil:
		source = buildBucket(sourceAPIGroup, sourceAPIVersion, rel)
		expectedSourceHashedName, err := GetObjectHashedName(source)
		if err != nil {
			return nil, fmt.Errorf("set chart source hashed name: %w", err)
		}
		source.SetName(expectedSourceHashedName)

	case chart.OCIRepositoryChartSource != nil:
		// oci is terminal on its own; no companion HelmChart is created.
		source = buildOCIRepository(sourceAPIGroup, sourceAPIVersion, rel)
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

	spec := unstructuredMap["spec"]

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
	digest, _, _ := unstructured.NestedString(obj.Object, "status", "artifact", "digest")

	chartPath, err := downloadArtifact(ctx, artifactURL, tempDir, httpRetry, httpTimeout)
	if err != nil {
		return nil, fmt.Errorf("download chart artifact: %w", err)
	}

	return &ChartResult{
		ChartPath: chartPath,
		Digest:    digest,
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

// TypescriptChartPath returns the chart path to give Nelm. Nelm builds the
// TypeScript bundle of a chart only from a chart directory, so when a bundle
// is missing or must be rebuilt, the archive is unpacked and the path of the
// chart directory is returned.
func TypescriptChartPath(chartPath string, tempDir string, ignoreBundleJS bool) (string, error) {
	build, err := typescriptBuildNeeded(chartPath, ignoreBundleJS)
	if err != nil {
		return "", err
	}

	if !build {
		return chartPath, nil
	}

	dir, err := os.MkdirTemp(tempDir, "chart-*")
	if err != nil {
		return "", fmt.Errorf("create chart directory: %w", err)
	}

	if err := chartutil.ExpandFile(dir, chartPath); err != nil {
		return "", fmt.Errorf("extract chart archive: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read chart directory: %w", err)
	}

	if len(entries) != 1 || !entries[0].IsDir() {
		return "", fmt.Errorf("extract chart archive: expected one chart directory, got %d entries", len(entries))
	}

	chartDir := filepath.Join(dir, entries[0].Name())

	// Nelm loads a chart directory unlike an archive: it applies .helmignore
	// again and resolves the dependencies in the lock files, from local paths
	// or the network. Drop these files, the packaged dependencies are in
	// charts/ already.
	for _, name := range []string{".helmignore", "Chart.lock", "requirements.lock"} {
		if err := os.Remove(filepath.Join(chartDir, name)); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("remove %s from chart directory: %w", name, err)
		}
	}

	return chartDir, nil
}

// chartFilePrefix matches the path of the top chart in its archive (empty)
// and of the subcharts stored as directories in it.
var chartFilePrefix = regexp.MustCompile(`^(charts/[^/]+/)*$`)

// typescriptBuildNeeded reports whether the archive has a chart or a
// directory subchart with TypeScript sources whose bundle is missing or must
// be rebuilt.
func typescriptBuildNeeded(chartPath string, ignoreBundleJS bool) (bool, error) {
	f, err := os.Open(chartPath)
	if err != nil {
		return false, fmt.Errorf("open chart archive: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return false, fmt.Errorf("read chart archive: %w", err)
	}

	// Nelm rejects charts whose files exceed this size when it loads them. The
	// tar headers come on top, so allow twice as much before giving up.
	limited := &io.LimitedReader{R: gz, N: 2 * archive.MaxDecompressedChartSize}
	tr := tar.NewReader(limited)

	files := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) && limited.N == 0 {
			return false, fmt.Errorf("read chart archive: larger than %d bytes", 2*archive.MaxDecompressedChartSize)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, fmt.Errorf("read chart archive: %w", err)
		}

		// Drop the top directory named after the chart.
		if _, name, found := strings.Cut(hdr.Name, "/"); found && strings.Contains(name, common.ChartTSSourceDir) {
			files[name] = true
		}
	}

	for name := range files {
		prefix, isTS := strings.CutSuffix(name, common.ChartTSSourceDir+common.ChartTSEntryPointTS)
		if !isTS {
			prefix, isTS = strings.CutSuffix(name, common.ChartTSSourceDir+common.ChartTSEntryPointJS)
		}

		if isTS && chartFilePrefix.MatchString(prefix) && (ignoreBundleJS || !files[prefix+common.ChartTSBundleFile]) {
			return true, nil
		}
	}

	return false, nil
}

type SourceNotReadyError struct {
	Message string
}

func (e *SourceNotReadyError) Error() string {
	return fmt.Sprintf("source not ready: %s", e.Message)
}
