/*
Copyright The Kubernetes Authors.

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
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

func TestExtractReadinessTaints(t *testing.T) {
	g := NewWithT(t)
	now := metav1.NewTime(time.Now())

	rules := []readinessv1alpha1.NodeReadinessRule{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "rule-b"},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				Taint: corev1.Taint{
					Key:    "readiness.k8s.io/storage-ready",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "rule-a"},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				Taint: corev1.Taint{
					Key:    "readiness.k8s.io/network-ready",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		{
			// Duplicate key - should be deduplicated
			ObjectMeta: metav1.ObjectMeta{Name: "rule-b-dup"},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				Taint: corev1.Taint{
					Key:    "readiness.k8s.io/storage-ready",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		{
			// Non-readiness prefix - should be excluded
			ObjectMeta: metav1.ObjectMeta{Name: "custom-taint"},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				Taint: corev1.Taint{
					Key:    "example.com/custom-ready",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		{
			// Effect is NoExecute - should be excluded
			ObjectMeta: metav1.ObjectMeta{Name: "no-execute-rule"},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				Taint: corev1.Taint{
					Key:    "readiness.k8s.io/eviction-ready",
					Effect: corev1.TaintEffectNoExecute,
				},
			},
		},
		{
			// Being deleted - should be excluded
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deleted-rule",
				DeletionTimestamp: &now,
			},
			Spec: readinessv1alpha1.NodeReadinessRuleSpec{
				Taint: corev1.Taint{
					Key:    "readiness.k8s.io/deleted-ready",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	taints := ExtractReadinessTaints(rules)
	g.Expect(taints).To(Equal([]string{
		"readiness.k8s.io/network-ready",
		"readiness.k8s.io/storage-ready",
	}))
}

func TestSyncTaintsConfigMap(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = readinessv1alpha1.AddToScheme(scheme)

	rule1 := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule1"},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			Taint: corev1.Taint{
				Key:    "readiness.k8s.io/cni-ready",
				Effect: corev1.TaintEffectNoSchedule,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rule1).
		Build()

	controller := &RuleReadinessController{
		Client:             fakeClient,
		Scheme:             scheme,
		configMapNamespace: "test-ns",
	}

	// 1. Initial sync: creates the ConfigMap
	err := controller.syncTaintsConfigMap(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	cm := &corev1.ConfigMap{}
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "readiness-taints", Namespace: "test-ns"}, cm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cm.Data["taint-keys"]).To(Equal("readiness.k8s.io/cni-ready"))

	// 2. Add second rule and sync: updates the ConfigMap
	rule2 := &readinessv1alpha1.NodeReadinessRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "rule2",
			Finalizers: []string{"readiness.node.x-k8s.io/finalizer"},
		},
		Spec: readinessv1alpha1.NodeReadinessRuleSpec{
			Taint: corev1.Taint{
				Key:    "readiness.k8s.io/csi-ready",
				Effect: corev1.TaintEffectNoSchedule,
			},
		},
	}
	err = fakeClient.Create(ctx, rule2)
	g.Expect(err).NotTo(HaveOccurred())

	err = controller.syncTaintsConfigMap(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	err = fakeClient.Get(ctx, client.ObjectKey{Name: "readiness-taints", Namespace: "test-ns"}, cm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cm.Data["taint-keys"]).To(Equal("readiness.k8s.io/cni-ready,readiness.k8s.io/csi-ready"))

	// 3. Repeated sync with unchanged rules: should succeed without error
	err = controller.syncTaintsConfigMap(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cm.Data["taint-keys"]).To(Equal("readiness.k8s.io/cni-ready,readiness.k8s.io/csi-ready"))

	// 4. Mark rule2 for deletion: with a finalizer, Delete sets DeletionTimestamp
	err = fakeClient.Delete(ctx, rule2)
	g.Expect(err).NotTo(HaveOccurred())

	err = controller.syncTaintsConfigMap(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	err = fakeClient.Get(ctx, client.ObjectKey{Name: "readiness-taints", Namespace: "test-ns"}, cm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cm.Data["taint-keys"]).To(Equal("readiness.k8s.io/cni-ready"))

	// 5. Remove finalizer from rule2 and delete rule1: all rules deleted -> empty string
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "rule2"}, rule2)
	g.Expect(err).NotTo(HaveOccurred())
	rule2.Finalizers = nil
	err = fakeClient.Update(ctx, rule2)
	g.Expect(err).NotTo(HaveOccurred())
	err = fakeClient.Delete(ctx, rule1)
	g.Expect(err).NotTo(HaveOccurred())

	err = controller.syncTaintsConfigMap(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	err = fakeClient.Get(ctx, client.ObjectKey{Name: "readiness-taints", Namespace: "test-ns"}, cm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cm.Data["taint-keys"]).To(Equal(""))
}
