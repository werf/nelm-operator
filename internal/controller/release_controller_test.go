/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
	"github.com/werf/nelm-operator/internal/config"
	"github.com/werf/nelm-operator/internal/source"
)

// These integration specs run against envtest (a real API server + etcd) but
// WITHOUT any FluxCD controllers running. That means the source CRs the
// operator creates (GitRepository/HelmRepository/OCIRepository/Bucket and the
// companion HelmChart) never reach a Ready state on their own. The specs
// therefore assert what the OPERATOR does: it creates the right typed CRs,
// stamps them with a controller owner reference back to the Release, wires the
// HelmChart sourceRef to the per-source object, and requeues (rather than
// fails) while the source is not ready.
//
// Notes / envtest gotchas baked into these specs:
//   - Owner references require the owner (Release) to exist in the API server
//     and to have a UID. We create the Release first, then re-Get it (to
//     populate UID) before calling source.EnsureInlineChart, which sets the
//     owner ref.
//   - The Release is created via the unstructured API with exactly one source
//     key under spec.chart. The typed ReleaseChart struct embeds all four
//     source structs by value (non-pointer), so a typed Create would serialize
//     empty companions and trip the "exactly one of" CEL rule. Unstructured
//     creation keeps the wire payload to a single source key.
func newOperatorConfig() config.OperatorConfig {
	return config.OperatorConfig{
		SourceAPIGroup:   "source.toolkit.fluxcd.io",
		SourceAPIVersion: "v1",
		HTTPRetry:        1,
		HTTPTimeout:      30 * time.Second,
		TempDir:          "",
	}
}

// createNamespace creates a namespace and ignores AlreadyExists so specs are
// independent and re-runnable.
func createNamespace(name string) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// createInlineRelease creates a Release with a single inline chart source via
// the unstructured API, then returns a typed Release re-fetched from the API
// server (so it carries a UID for owner references).
func createInlineRelease(namespace, name, sourceKey string, sourceSpec map[string]any) *nelmv1alpha1.Release {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(nelmv1alpha1.GroupVersion.WithKind("Release"))
	u.SetNamespace(namespace)
	u.SetName(name)
	Expect(unstructured.SetNestedMap(u.Object, map[string]any{
		sourceKey: sourceSpec,
	}, "spec", "chart")).To(Succeed())
	Expect(unstructured.SetNestedField(u.Object, "1m", "spec", "interval")).To(Succeed())

	Expect(k8sClient.Create(ctx, u)).To(Succeed())

	rel := &nelmv1alpha1.Release{}
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, rel)
	}).Should(Succeed())
	Expect(rel.UID).NotTo(BeEmpty(), "Release must have a UID before owner refs can be set")
	return rel
}

// assertControllerOwnerRef verifies the created CR is controller-owned by the
// Release with both Controller and BlockOwnerDeletion set true.
func assertControllerOwnerRef(obj client.Object, rel *nelmv1alpha1.Release) {
	owners := obj.GetOwnerReferences()
	Expect(owners).To(HaveLen(1))
	owner := owners[0]
	Expect(owner.Name).To(Equal(rel.Name))
	Expect(owner.Kind).To(Equal("Release"))
	Expect(owner.UID).To(Equal(rel.UID))
	Expect(owner.Controller).NotTo(BeNil())
	Expect(*owner.Controller).To(BeTrue())
	Expect(owner.BlockOwnerDeletion).NotTo(BeNil())
	Expect(*owner.BlockOwnerDeletion).To(BeTrue())
}

var _ = Describe("Release inline chart", func() {
	var r *ReleaseReconciler

	BeforeEach(func() {
		r = &ReleaseReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
			Config: newOperatorConfig(),
		}
	})

	Context("git source", func() {
		It("creates a GitRepository and companion HelmChart owned by the Release", func() {
			const ns = "git-ns"
			const relName = "gitrel"
			createNamespace(ns)
			rel := createInlineRelease(ns, relName, "git", map[string]any{
				"url":      "https://github.com/example/repo",
				"path":     "./charts/app",
				"interval": "5m",
			})

			ref, err := source.EnsureInlineChart(ctx, r.Client, r.Scheme, rel)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.Kind).To(Equal(sourcev1.HelmChartKind))
			Expect(ref.Name).To(Equal("git-ns-gitrel"))

			repo := &sourcev1.GitRepository{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "git-ns-gitrel"}, repo)
			}).Should(Succeed())
			assertControllerOwnerRef(repo, rel)

			hc := &sourcev1.HelmChart{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "git-ns-gitrel"}, hc)
			}).Should(Succeed())
			assertControllerOwnerRef(hc, rel)
			Expect(hc.Spec.SourceRef.Kind).To(Equal(sourcev1.GitRepositoryKind))
			Expect(hc.Spec.SourceRef.Name).To(Equal("git-ns-gitrel"))
		})
	})

	Context("repo (HelmRepository) source", func() {
		It("creates a HelmRepository and companion HelmChart owned by the Release", func() {
			const ns = "repo-ns"
			const relName = "reporel"
			createNamespace(ns)
			rel := createInlineRelease(ns, relName, "repo", map[string]any{
				"url":      "https://charts.example.com",
				"name":     "podinfo",
				"version":  "6.7.1",
				"interval": "5m",
			})

			ref, err := source.EnsureInlineChart(ctx, r.Client, r.Scheme, rel)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.Kind).To(Equal(sourcev1.HelmChartKind))
			Expect(ref.Name).To(Equal("repo-ns-reporel"))

			repo := &sourcev1.HelmRepository{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "repo-ns-reporel"}, repo)
			}).Should(Succeed())
			assertControllerOwnerRef(repo, rel)

			hc := &sourcev1.HelmChart{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "repo-ns-reporel"}, hc)
			}).Should(Succeed())
			assertControllerOwnerRef(hc, rel)
			Expect(hc.Spec.SourceRef.Kind).To(Equal(sourcev1.HelmRepositoryKind))
			Expect(hc.Spec.SourceRef.Name).To(Equal("repo-ns-reporel"))
		})
	})

	Context("bucket source", func() {
		It("creates a Bucket and companion HelmChart owned by the Release", func() {
			const ns = "bucket-ns"
			const relName = "bucketrel"
			createNamespace(ns)
			rel := createInlineRelease(ns, relName, "bucket", map[string]any{
				"endpoint":   "minio.example.com",
				"bucketName": "charts",
				"path":       "./app",
				"interval":   "5m",
			})

			ref, err := source.EnsureInlineChart(ctx, r.Client, r.Scheme, rel)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.Kind).To(Equal(sourcev1.HelmChartKind))
			Expect(ref.Name).To(Equal("bucket-ns-bucketrel"))

			bucket := &sourcev1.Bucket{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "bucket-ns-bucketrel"}, bucket)
			}).Should(Succeed())
			assertControllerOwnerRef(bucket, rel)

			hc := &sourcev1.HelmChart{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "bucket-ns-bucketrel"}, hc)
			}).Should(Succeed())
			assertControllerOwnerRef(hc, rel)
			Expect(hc.Spec.SourceRef.Kind).To(Equal(sourcev1.BucketKind))
			Expect(hc.Spec.SourceRef.Name).To(Equal("bucket-ns-bucketrel"))
		})
	})

	Context("oci source", func() {
		It("creates only an OCIRepository (no HelmChart) owned by the Release", func() {
			const ns = "oci-ns"
			const relName = "ocirel"
			createNamespace(ns)
			rel := createInlineRelease(ns, relName, "oci", map[string]any{
				"url":      "oci://registry.example.com/charts/app",
				"tag":      "1.0.0",
				"interval": "5m",
			})

			ref, err := source.EnsureInlineChart(ctx, r.Client, r.Scheme, rel)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.Kind).To(Equal(sourcev1.OCIRepositoryKind))
			Expect(ref.Name).To(Equal("oci-ns-ocirel"))

			oci := &sourcev1.OCIRepository{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "oci-ns-ocirel"}, oci)
			}).Should(Succeed())
			assertControllerOwnerRef(oci, rel)

			// oci is terminal on its own: NO companion HelmChart must be created.
			hc := &sourcev1.HelmChart{}
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "oci-ns-ocirel"}, hc)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no HelmChart must exist for an oci inline source")
		})
	})

	Context("source not ready", func() {
		// With no FluxCD controllers running in envtest, the HelmChart the
		// operator creates never gets a Ready condition. We assert the
		// not-ready behavior at two complementary points:
		//
		//  1. The source boundary: source.ResolveChartSource returns a
		//     *source.SourceNotReadyError (exercised directly).
		//  2. The reconciler boundary: reconcileInstall maps that error to
		//     ctrl.Result{RequeueAfter: 15s} with a nil error and does NOT
		//     mark the Release failed/stalled.
		//
		// We drive reconcileInstall directly rather than the full Reconcile
		// because reconcileInstall returns inside the not-ready branch BEFORE
		// reaching nelm's install machinery, which is not exercisable in
		// envtest. (Reconcile would also add a finalizer and requeue on its
		// first call, which is orthogonal to the not-ready mapping.)
		It("resolve returns SourceNotReadyError and reconcileInstall requeues without failing", func() {
			const ns = "notready-ns"
			const relName = "notreadyrel"
			createNamespace(ns)
			rel := createInlineRelease(ns, relName, "repo", map[string]any{
				"url":      "https://charts.example.com",
				"name":     "podinfo",
				"version":  "6.7.1",
				"interval": "5m",
			})

			// 1. Source boundary: the resolver classifies the not-ready source.
			tempDir := GinkgoT().TempDir()
			_, err := source.ResolveChartSource(ctx, k8sClient, scheme.Scheme, rel,
				r.Config.SourceAPIGroup, r.Config.SourceAPIVersion, tempDir,
				r.Config.HTTPRetry, r.Config.HTTPTimeout)
			Expect(err).To(HaveOccurred())
			var notReady *source.SourceNotReadyError
			Expect(errors.As(err, &notReady)).To(BeTrue(),
				"expected a *source.SourceNotReadyError, got %T: %v", err, err)

			// 2. Reconciler boundary: not-ready maps to a 15s requeue, no failure.
			result, err := r.reconcileInstall(ctx, rel)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15 * time.Second))

			// The Release must NOT have been marked failed/stalled.
			fresh := &nelmv1alpha1.Release{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: relName}, fresh)).To(Succeed())
			ready := apimeta.FindStatusCondition(fresh.Status.Conditions, "Ready")
			if ready != nil {
				Expect(ready.Reason).NotTo(Equal("InstallFailed"),
					"not-ready source must not set Ready=False/InstallFailed")
			}
			stalled := apimeta.FindStatusCondition(fresh.Status.Conditions, "Stalled")
			if stalled != nil {
				Expect(stalled.Status).NotTo(Equal(metav1.ConditionTrue),
					"not-ready source must not stall the Release")
			}
		})
	})
})
