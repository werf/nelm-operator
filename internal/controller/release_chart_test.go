package controller

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/werf/logboek"
	"github.com/werf/nelm/v2/pkg/action"
	"github.com/werf/nelm/v2/pkg/featgate"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm-operator/internal/source"
	"github.com/werf/nelm-operator/internal/values"
)

// chartArchive packs files into a gzipped chart archive under a top-level
// directory named after the chart, as Helm and Flux do.
func chartArchive(name string, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for path, content := range files {
		Expect(tw.WriteHeader(&tar.Header{
			Name:    name + "/" + path,
			Mode:    0o644,
			Size:    int64(len(content)),
			ModTime: time.Unix(0, 0),
		})).To(Succeed())
		_, err := tw.Write([]byte(content))
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())

	return buf.Bytes()
}

// serveReadyHelmChart serves archive over HTTP and returns a client holding a
// Ready HelmChart whose artifact points at it.
func serveReadyHelmChart(archive []byte, helmChartSpec sourcev1.HelmChartSpec) (*fake.ClientBuilder, *nelmv1alpha1.ChartSourceRef) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	DeferCleanup(srv.Close)

	scheme := runtime.NewScheme()
	Expect(nelmv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(sourcev1.AddToScheme(scheme)).To(Succeed())

	helmChart := &sourcev1.HelmChart{
		ObjectMeta: metav1.ObjectMeta{Name: "chart", Namespace: "default"},
		Spec:       helmChartSpec,
		Status: sourcev1.HelmChartStatus{
			Artifact: &meta.Artifact{URL: srv.URL + "/chart.tgz", Revision: "1.0.0", Digest: "sha256:0"},
			Conditions: []metav1.Condition{{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Succeeded",
				LastTransitionTime: metav1.Now(),
			}},
		},
	}

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(helmChart)

	return builder, &nelmv1alpha1.ChartSourceRef{Kind: sourcev1.HelmChartKind, Name: "chart", Namespace: "default"}
}

var _ = Describe("chart source valuesFiles", func() {
	var mergedArtifact []byte

	BeforeEach(func() {
		// Flux merges HelmChart.spec.valuesFiles into values.yaml of the artifact
		// it packages; files listed from inside the chart stay in it as well.
		mergedArtifact = chartArchive("app", map[string]string{
			"Chart.yaml":       "apiVersion: v2\nname: app\nversion: 1.0.0\n",
			"values.yaml":      "replicaCount: 3\nenv: prod\n",
			"values-prod.yaml": "replicaCount: 3\nenv: prod\n",
			"templates/cm.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
data:
  replicaCount: "{{ .Values.replicaCount }}"
  env: "{{ .Values.env }}"
`,
		})
	})

	render := func(inline string) map[string]any {
		ctx := context.Background()
		builder, chartRef := serveReadyHelmChart(mergedArtifact, sourcev1.HelmChartSpec{
			Chart:       "app",
			ValuesFiles: []string{"values.yaml", "values-prod.yaml"},
		})
		cl := builder.Build()

		rel := &nelmv1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"}}
		if inline != "" {
			rel.Spec.Values = &apiextensionsv1.JSON{Raw: []byte(inline)}
		}

		tempDir := GinkgoT().TempDir()
		chartResult, err := source.ResolveChartRef(ctx, cl, sourcev1.GroupVersion.Group, sourcev1.GroupVersion.Version, chartRef, tempDir, 0, 10*time.Second)
		Expect(err).NotTo(HaveOccurred())

		resolved, err := values.Resolve(ctx, cl, rel, tempDir)
		Expect(err).NotTo(HaveOccurred())

		r := &ReleaseReconciler{}
		result, err := action.ChartRender(ctx, action.ChartRenderOptions{
			ValuesOptions:    r.buildValuesOptions(rel, resolved),
			Chart:            chartResult.ChartPath,
			ReleaseName:      rel.Name,
			ReleaseNamespace: rel.Namespace,
			OutputNoPrint:    true,
			TempDirPath:      tempDir,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Resources).To(HaveLen(1))

		data, ok := result.Resources[0].Unstruct.Object["data"].(map[string]any)
		Expect(ok).To(BeTrue())

		return data
	}

	It("renders the values Flux merged into the artifact", func() {
		Expect(render("")).To(Equal(map[string]any{"replicaCount": "3", "env": "prod"}))
	})

	It("lets spec.values override the merged values", func() {
		Expect(render(`{"env":"staging"}`)).To(Equal(map[string]any{"replicaCount": "3", "env": "staging"}))
	})
})

var _ = Describe("TypeScript charts", func() {
	configMapJS := func(from string) string {
		return `Deno.writeTextFileSync("output.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ts\ndata:\n  from: ` + from + `\n");` + "\n"
	}

	// ts/dist/bundle.js and ts/src/index.ts render different data, so the
	// result shows which of them Nelm ran.
	tsChartFiles := func(withBundle bool) map[string]string {
		files := map[string]string{
			"Chart.yaml": "apiVersion: v2\nname: ts-app\nversion: 1.0.0\ndependencies:\n- name: common\n  repository: file://../common\n  version: 1.0.0\n",
			"Chart.lock": "dependencies:\n- name: common\n  repository: file://../common\n  version: 1.0.0\ndigest: sha256:0\n",
			"charts/common-1.0.0.tgz": string(chartArchive("common", map[string]string{
				"Chart.yaml": "apiVersion: v2\nname: common\nversion: 1.0.0\n",
			})),
			".helmignore":      "templates/\n",
			"templates/a.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: template\n",
			"ts/src/index.ts":  configMapJS("source"),
		}
		if withBundle {
			files["ts/dist/bundle.js"] = configMapJS("bundle")
		}

		return files
	}

	BeforeEach(func() {
		GinkgoT().Setenv(featgate.FeatGateTypescript.EnvVarName(), "true")
	})

	// renderFrom prepares the archive as the operator does and renders it,
	// returning the data of the TypeScript ConfigMap and the names of all
	// rendered resources.
	renderFrom := func(files map[string]string, ignoreBundleJS bool) (any, []string) {
		tempDir := GinkgoT().TempDir()
		archivePath := filepath.Join(tempDir, "chart.tgz")
		Expect(os.WriteFile(archivePath, chartArchive("ts-app", files), 0o644)).To(Succeed())

		chartPath, err := source.TypescriptChartPath(archivePath, tempDir, ignoreBundleJS)
		Expect(err).NotTo(HaveOccurred())

		// Without Deno on PATH, Nelm downloads its pinned Deno release into the
		// user cache directory, like the operator does outside its image.
		denoPath, _ := exec.LookPath("deno")
		ctx := logboek.NewContext(context.Background(), logboek.DefaultLogger())
		result, err := action.ChartRender(ctx, action.ChartRenderOptions{
			Chart:            chartPath,
			DenoBinaryPath:   denoPath,
			IgnoreBundleJS:   ignoreBundleJS,
			ReleaseName:      "ts-app",
			ReleaseNamespace: "default",
			OutputNoPrint:    true,
			TempDirPath:      tempDir,
		})
		Expect(err).NotTo(HaveOccurred())

		var from any
		names := make([]string, 0, len(result.Resources))
		for _, res := range result.Resources {
			names = append(names, res.Unstruct.GetName())
			if res.Unstruct.GetName() == "ts" {
				from = res.Unstruct.Object["data"].(map[string]any)["from"]
			}
		}

		return from, names
	}

	It("renders ts/dist/bundle.js from the chart archive", func() {
		from, names := renderFrom(tsChartFiles(true), false)
		Expect(from).To(Equal("bundle"))
		Expect(names).To(ConsistOf("ts", "template"))
	})

	It("rebuilds the bundle from ts/src with ignoreBundleJS", func() {
		from, names := renderFrom(tsChartFiles(true), true)
		Expect(from).To(Equal("source"))
		Expect(names).To(ConsistOf("ts", "template"))
	})

	It("builds the bundle from ts/src when the chart has none", func() {
		from, names := renderFrom(tsChartFiles(false), false)
		Expect(from).To(Equal("source"))
		Expect(names).To(ConsistOf("ts", "template"))
	})

	It("builds the bundle of a subchart stored as a directory", func() {
		from, names := renderFrom(map[string]string{
			"Chart.yaml":                   "apiVersion: v2\nname: ts-app\nversion: 1.0.0\n",
			"charts/child/Chart.yaml":      "apiVersion: v2\nname: child\nversion: 1.0.0\n",
			"charts/child/ts/src/index.ts": configMapJS("child"),
		}, false)
		Expect(from).To(Equal("child"))
		Expect(names).To(ConsistOf("ts"))
	})

	It("passes a chart without TypeScript sources as the archive", func() {
		tempDir := GinkgoT().TempDir()
		archivePath := filepath.Join(tempDir, "chart.tgz")
		Expect(os.WriteFile(archivePath, chartArchive("app", map[string]string{
			"Chart.yaml": "apiVersion: v2\nname: app\nversion: 1.0.0\n",
		}), 0o644)).To(Succeed())

		Expect(source.TypescriptChartPath(archivePath, tempDir, true)).To(Equal(archivePath))
	})
})
