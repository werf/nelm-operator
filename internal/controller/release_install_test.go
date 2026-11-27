package controller

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/werf/logboek"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/werf/nelm/v2/pkg/action"
	"github.com/werf/nelm/v2/pkg/common"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm-operator/internal/values"
)

// installWithPlan mirrors the plan-then-install sequence of reconcileInstall,
// pointing Nelm at the envtest API server instead of the in-cluster one.
func installWithPlan(r *ReleaseReconciler, rel *nelmv1alpha1.Release, chartPath string, resolved *values.ResolvedValues) {
	ctx := logboek.NewContext(ctx, logboek.DefaultLogger())
	kubeOpts := common.KubeConnectionOptions{KubeConfigBase64: envtestKubeConfigBase64()}

	planOpts, err := r.buildPlanInstallOptions(rel, chartPath, GinkgoT().TempDir(), resolved)
	Expect(err).NotTo(HaveOccurred())
	planOpts.KubeConnectionOptions = kubeOpts

	planArtifact, err := action.ReleasePlanInstall(ctx, rel.Name, rel.GetReleaseNamespace(), planOpts)
	Expect(err).To(Or(MatchError(action.ErrResourceChangesPlanned), MatchError(action.ErrReleaseInstallPlanned)))

	installOpts, err := r.buildInstallOptions(rel, chartPath, GinkgoT().TempDir(), resolved)
	Expect(err).NotTo(HaveOccurred())
	installOpts.KubeConnectionOptions = kubeOpts
	installOpts.LegacyPlanArtifact = planArtifact
	installOpts.PlanArtifactLifetime = 10 * time.Minute

	_, err = action.ReleaseInstall(ctx, rel.Name, rel.GetReleaseNamespace(), installOpts)
	Expect(err).NotTo(HaveOccurred())
}

func envtestKubeConfigBase64() string {
	kubeConfig := clientcmdapi.NewConfig()
	kubeConfig.Clusters["envtest"] = &clientcmdapi.Cluster{Server: cfg.Host, CertificateAuthorityData: cfg.CAData}
	kubeConfig.AuthInfos["envtest"] = &clientcmdapi.AuthInfo{ClientCertificateData: cfg.CertData, ClientKeyData: cfg.KeyData}
	kubeConfig.Contexts["envtest"] = &clientcmdapi.Context{Cluster: "envtest", AuthInfo: "envtest"}
	kubeConfig.CurrentContext = "envtest"

	data, err := clientcmd.Write(*kubeConfig)
	Expect(err).NotTo(HaveOccurred())

	return base64.StdEncoding.EncodeToString(data)
}

func createTestNamespace() string {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "release-install-"}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ns))).To(Succeed())
	})

	return ns.Name
}

func writeConfigMapChart(dir string) string {
	chartPath := filepath.Join(dir, "chart")
	Expect(os.MkdirAll(filepath.Join(chartPath, "templates"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte("apiVersion: v2\nname: history\nversion: 0.1.0\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(chartPath, "templates", "cm.yaml"), []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\ndata:\n  rev: {{ .Values.rev | quote }}\n",
	), 0o644)).To(Succeed())

	return chartPath
}

var _ = Describe("Release install through a plan", func() {
	const upgrades = 12

	DescribeTable("trims release history",
		func(storage *nelmv1alpha1.ReleaseStorageConfig, expectedRevisions int) {
			dir := GinkgoT().TempDir()
			chartPath := writeConfigMapChart(dir)

			r := &ReleaseReconciler{}
			rel := &nelmv1alpha1.Release{
				ObjectMeta: metav1.ObjectMeta{Name: "history", Namespace: createTestNamespace()},
				Spec:       nelmv1alpha1.ReleaseSpec{ReleaseStorage: storage},
			}

			for i := 1; i <= upgrades; i++ {
				valuesFile := filepath.Join(dir, fmt.Sprintf("values-%d.yaml", i))
				Expect(os.WriteFile(valuesFile, fmt.Appendf(nil, "rev: %d\n", i), 0o644)).To(Succeed())

				installWithPlan(r, rel, chartPath, &values.ResolvedValues{ValuesFiles: []string{valuesFile}})
			}

			var secrets corev1.SecretList
			Expect(k8sClient.List(ctx, &secrets, client.InNamespace(rel.Namespace),
				client.MatchingLabels{"owner": "helm", "name": rel.Name})).To(Succeed())
			Expect(secrets.Items).To(HaveLen(expectedRevisions))
		},
		Entry("to 10 revisions when spec.releaseStorage is not set", nil, common.DefaultReleaseHistoryLimit),
		Entry("never when historyLimit is explicitly 0", &nelmv1alpha1.ReleaseStorageConfig{HistoryLimit: 0}, upgrades),
	)
})
