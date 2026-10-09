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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/werf/nelm/v2/pkg/action"
	"github.com/werf/nelm/v2/pkg/resource/spec"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

var _ = Describe("ownershipLabels", func() {
	It("forces the ownership marker on an empty label set", func() {
		got := ownershipLabels(nil)
		Expect(got).To(HaveKeyWithValue(ownershipMarkerKey, ownershipMarkerValue))
		Expect(got).To(HaveLen(1))
	})

	It("preserves user labels while forcing the marker", func() {
		user := map[string]string{"team": "platform", "env": "prod"}
		got := ownershipLabels(user)
		Expect(got).To(HaveKeyWithValue("team", "platform"))
		Expect(got).To(HaveKeyWithValue("env", "prod"))
		Expect(got).To(HaveKeyWithValue(ownershipMarkerKey, ownershipMarkerValue))
	})

	It("overrides a user-supplied value for the reserved marker key", func() {
		got := ownershipLabels(map[string]string{ownershipMarkerKey: "someone-else"})
		Expect(got).To(HaveKeyWithValue(ownershipMarkerKey, ownershipMarkerValue))
	})

	It("does not mutate the input map", func() {
		user := map[string]string{"team": "platform"}
		_ = ownershipLabels(user)
		Expect(user).NotTo(HaveKey(ownershipMarkerKey))
		Expect(user).To(HaveLen(1))
	})
})

var _ = Describe("detectForeignChange", func() {
	marked := func(revision int) *action.ReleaseGetResultRelease {
		return &action.ReleaseGetResultRelease{
			Revision:      revision,
			StorageLabels: map[string]string{ownershipMarkerKey: ownershipMarkerValue},
		}
	}
	unmarked := func(revision int) *action.ReleaseGetResultRelease {
		return &action.ReleaseGetResultRelease{Revision: revision}
	}

	It("treats a first reconcile (no recorded revision) as adoption, not foreign", func() {
		Expect(detectForeignChange(0, unmarked(3))).To(BeFalse())
	})

	It("flags an absent release with a recorded revision as foreign", func() {
		Expect(detectForeignChange(2, nil)).To(BeTrue())
	})

	It("flags a bumped storage revision as foreign", func() {
		Expect(detectForeignChange(2, marked(3))).To(BeTrue())
	})

	It("flags a matching revision missing the marker as foreign", func() {
		Expect(detectForeignChange(2, unmarked(2))).To(BeTrue())
	})

	It("does not flag the operator's own marked steady state", func() {
		Expect(detectForeignChange(2, marked(2))).To(BeFalse())
	})
})

var _ = Describe("emitEvent", func() {
	It("is a no-op when the recorder is nil", func() {
		r := &ReleaseReconciler{}
		Expect(func() {
			r.emitEvent(&nelmv1alpha1.Release{}, corev1.EventTypeNormal, reasonForeignChangeAdopted, "msg")
		}).NotTo(Panic())
	})

	It("records an event when a recorder is set", func() {
		rec := record.NewFakeRecorder(1)
		r := &ReleaseReconciler{EventRecorder: rec}
		r.emitEvent(&nelmv1alpha1.Release{}, corev1.EventTypeWarning, reasonForeignChangeReconciled, "diverged")
		Eventually(rec.Events).Should(Receive(ContainSubstring(reasonForeignChangeReconciled)))
	})
})

var _ = Describe("buildRuntimeOptions", func() {
	It("stamps the ownership marker and defaults ForceAdoption on", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{}
		opts, err := r.buildRuntimeOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.ReleaseLabels).To(HaveKeyWithValue(ownershipMarkerKey, ownershipMarkerValue))
		Expect(opts.ForceAdoption).To(BeTrue())
	})

	It("honours the NoForceAdoption opt-out", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				Install: &nelmv1alpha1.InstallConfig{NoForceAdoption: true},
			},
		}
		opts, err := r.buildRuntimeOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.ForceAdoption).To(BeFalse())
	})

	It("maps spec.diffPatches into a patches file passed to nelm", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				DiffPatches: []nelmv1alpha1.Patch{
					{
						Match: nelmv1alpha1.PatchMatcher{
							Kinds:  []string{"Deployment"},
							Charts: []string{"cache"},
							Labels: map[string]string{"tier": "backend"},
						},
						Type:  "jq",
						Patch: "del(.spec.replicas)",
					},
				},
			},
		}

		opts, err := r.buildRuntimeOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PatchesFiles).To(HaveLen(1))

		parsed, err := spec.LoadPatchesFiles(opts.PatchesFiles)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Render).To(BeEmpty())
		Expect(parsed.Diff).To(HaveLen(1))
		Expect(parsed.Diff[0].Type).To(Equal(spec.PatchTypeJQ))
		Expect(parsed.Diff[0].Patch).To(Equal("del(.spec.replicas)"))
		Expect(parsed.Diff[0].Match.Kinds).To(Equal([]string{"Deployment"}))
		Expect(parsed.Diff[0].Match.Charts).To(Equal([]string{"cache"}))
		Expect(parsed.Diff[0].Match.Labels).To(HaveKeyWithValue("tier", "backend"))
	})

	It("maps spec.renderPatches into the same patches file passed to nelm", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				RenderPatches: []nelmv1alpha1.Patch{
					{
						Match: nelmv1alpha1.PatchMatcher{
							Kinds:      []string{"Deployment"},
							Names:      []string{"web"},
							Namespaces: []string{"prod"},
							Groups:     []string{"apps"},
							Versions:   []string{"v1"},
							Charts:     []string{"cache"},
							Labels:     map[string]string{"tier": "backend"},
							Annotations: map[string]string{
								"nelm.werf.io/patched": "true",
							},
						},
						Type:  "jq",
						Patch: ".spec.replicas = 3",
					},
				},
				DiffPatches: []nelmv1alpha1.Patch{{Patch: "del(.spec.replicas)"}},
			},
		}

		opts, err := r.buildRuntimeOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PatchesFiles).To(HaveLen(1))

		parsed, err := spec.LoadPatchesFiles(opts.PatchesFiles)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Render).To(HaveLen(1))
		Expect(parsed.Render[0].Type).To(Equal(spec.PatchTypeJQ))
		Expect(parsed.Render[0].Patch).To(Equal(".spec.replicas = 3"))
		Expect(parsed.Render[0].Match.Kinds).To(Equal([]string{"Deployment"}))
		Expect(parsed.Render[0].Match.Names).To(Equal([]string{"web"}))
		Expect(parsed.Render[0].Match.Namespaces).To(Equal([]string{"prod"}))
		Expect(parsed.Render[0].Match.Groups).To(Equal([]string{"apps"}))
		Expect(parsed.Render[0].Match.Versions).To(Equal([]string{"v1"}))
		Expect(parsed.Render[0].Match.Charts).To(Equal([]string{"cache"}))
		Expect(parsed.Render[0].Match.Labels).To(HaveKeyWithValue("tier", "backend"))
		Expect(parsed.Render[0].Match.Annotations).To(HaveKeyWithValue("nelm.werf.io/patched", "true"))
		Expect(parsed.Diff).To(HaveLen(1))
		Expect(parsed.Diff[0].Patch).To(Equal("del(.spec.replicas)"))
	})

	It("preserves the declared order of render patches", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				RenderPatches: []nelmv1alpha1.Patch{
					{Patch: ".spec.replicas = 1"},
					{Patch: ".spec.replicas += 1"},
				},
			},
		}

		opts, err := r.buildRuntimeOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())

		parsed, err := spec.LoadPatchesFiles(opts.PatchesFiles)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Render).To(HaveLen(2))
		Expect(parsed.Render[0].Patch).To(Equal(".spec.replicas = 1"))
		Expect(parsed.Render[1].Patch).To(Equal(".spec.replicas += 1"))
	})

	It("sets no patches files when neither renderPatches nor diffPatches is set", func() {
		r := &ReleaseReconciler{}
		opts, err := r.buildRuntimeOptions(&nelmv1alpha1.Release{}, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PatchesFiles).To(BeEmpty())
	})

	It("maps install.noDefaultPatches to DefaultPatchesDisable", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				Install: &nelmv1alpha1.InstallConfig{NoDefaultPatches: true},
			},
		}
		opts, err := r.buildRuntimeOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.DefaultPatchesDisable).To(BeTrue())
	})
})

var _ = Describe("buildRollbackOptions", func() {
	It("stamps the ownership marker and defaults ForceAdoption on", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{}
		opts, err := r.buildRollbackOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.ReleaseLabels).To(HaveKeyWithValue(ownershipMarkerKey, ownershipMarkerValue))
		Expect(opts.ForceAdoption).To(BeTrue())
	})

	It("maps only spec.diffPatches into a patches file", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				RenderPatches: []nelmv1alpha1.Patch{{Patch: ".spec.replicas = 3"}},
				DiffPatches:   []nelmv1alpha1.Patch{{Patch: "del(.spec.replicas)"}},
			},
		}
		opts, err := r.buildRollbackOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PatchesFiles).To(HaveLen(1))

		parsed, err := spec.LoadPatchesFiles(opts.PatchesFiles)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Render).To(BeEmpty())
		Expect(parsed.Diff).To(HaveLen(1))
	})

	It("writes no patches file when only spec.renderPatches is set", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				RenderPatches: []nelmv1alpha1.Patch{{Patch: ".spec.replicas = 3"}},
			},
		}
		opts, err := r.buildRollbackOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PatchesFiles).To(BeEmpty())
	})

	It("maps rollback.noDefaultPatches to DefaultPatchesDisable", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				Rollback: &nelmv1alpha1.RollbackConfig{NoDefaultPatches: true},
			},
		}
		opts, err := r.buildRollbackOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.DefaultPatchesDisable).To(BeTrue())
	})
})

var _ = Describe("buildUninstallOptions", func() {
	It("maps only spec.diffPatches into a patches file", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				RenderPatches: []nelmv1alpha1.Patch{{Patch: ".spec.replicas = 3"}},
				DiffPatches:   []nelmv1alpha1.Patch{{Patch: "del(.spec.replicas)"}},
			},
		}
		opts, err := r.buildUninstallOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PatchesFiles).To(HaveLen(1))

		parsed, err := spec.LoadPatchesFiles(opts.PatchesFiles)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Render).To(BeEmpty())
		Expect(parsed.Diff).To(HaveLen(1))
	})

	It("writes no patches file when only spec.renderPatches is set", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				RenderPatches: []nelmv1alpha1.Patch{{Patch: ".spec.replicas = 3"}},
			},
		}
		opts, err := r.buildUninstallOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PatchesFiles).To(BeEmpty())
	})

	It("maps uninstall.noDefaultPatches to DefaultPatchesDisable", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{
			Spec: nelmv1alpha1.ReleaseSpec{
				Uninstall: &nelmv1alpha1.UninstallConfig{NoDefaultPatches: true},
			},
		}
		opts, err := r.buildUninstallOptions(rel, GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.DefaultPatchesDisable).To(BeTrue())
	})
})

var _ = Describe("Reconcile spec.suspend", func() {
	It("skips reconciliation and emits no event when suspended", func() {
		scheme := runtime.NewScheme()
		Expect(nelmv1alpha1.AddToScheme(scheme)).To(Succeed())

		rel := &nelmv1alpha1.Release{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "suspended",
				Namespace:  "default",
				Generation: 3,
			},
			Spec: nelmv1alpha1.ReleaseSpec{Suspend: true},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rel).
			WithStatusSubresource(rel).
			Build()
		rec := record.NewFakeRecorder(4)
		r := &ReleaseReconciler{Client: cl, Scheme: scheme, EventRecorder: rec}

		res, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "suspended", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		Consistently(rec.Events).ShouldNot(Receive())

		var got nelmv1alpha1.Release
		Expect(cl.Get(context.Background(), types.NamespacedName{Name: "suspended", Namespace: "default"}, &got)).To(Succeed())
		Expect(got.Finalizers).To(BeEmpty())
		Expect(got.Status.Revision).To(BeZero())
	})
})
