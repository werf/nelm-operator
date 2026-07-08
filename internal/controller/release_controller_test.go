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

	"github.com/werf/nelm/pkg/action"

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
		opts := r.buildRuntimeOptions(rel)
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
		Expect(r.buildRuntimeOptions(rel).ForceAdoption).To(BeFalse())
	})
})

var _ = Describe("buildRollbackOptions", func() {
	It("stamps the ownership marker and defaults ForceAdoption on", func() {
		r := &ReleaseReconciler{}
		rel := &nelmv1alpha1.Release{}
		opts := r.buildRollbackOptions(rel, GinkgoT().TempDir())
		Expect(opts.ReleaseLabels).To(HaveKeyWithValue(ownershipMarkerKey, ownershipMarkerValue))
		Expect(opts.ForceAdoption).To(BeTrue())
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
		Expect(res.Requeue).To(BeFalse())
		Expect(res.RequeueAfter).To(BeZero())
		Consistently(rec.Events).ShouldNot(Receive())

		var got nelmv1alpha1.Release
		Expect(cl.Get(context.Background(), types.NamespacedName{Name: "suspended", Namespace: "default"}, &got)).To(Succeed())
		Expect(got.Finalizers).To(BeEmpty())
		Expect(got.Status.Revision).To(BeZero())
	})
})
