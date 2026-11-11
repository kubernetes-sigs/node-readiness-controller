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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// recordingQueue intercepts AddAfter to capture enqueued delays while delegating
// synchronous queue operations to controllertest.Queue. The implementation inherits
// the controllertest.Queue implementation.
type recordingQueue struct {
	*controllertest.Queue
	delays []time.Duration
}

// AddAfter overrides the controllertest.Queue AddAfter to capture the delay before
// calling controllertest.Queue.AddAfter.
func (q *recordingQueue) AddAfter(item reconcile.Request, duration time.Duration) {
	q.delays = append(q.delays, duration)
	q.Queue.AddAfter(item, duration)
}

func TestNodeToRuleEventHandler_enqueue(t *testing.T) {
	tests := []struct {
		name  string
		delay time.Duration
		rules []*readinessv1alpha1.NodeReadinessRule
		obj   client.Object
		want  []reconcile.Request
	}{
		{
			name:  "Object is nil",
			rules: []*readinessv1alpha1.NodeReadinessRule{{ObjectMeta: metav1.ObjectMeta{Name: "rule-1"}}},
			obj:   nil,
		},
		{
			name:  "Object is not a Node",
			rules: []*readinessv1alpha1.NodeReadinessRule{{ObjectMeta: metav1.ObjectMeta{Name: "rule-1"}}},
			obj:   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
		},
		{
			name:  "Object is a nil typecasted as Node",
			rules: []*readinessv1alpha1.NodeReadinessRule{{ObjectMeta: metav1.ObjectMeta{Name: "rule-1"}}},
			obj:   (*corev1.Node)(nil),
		},
		{
			name:  "Node matches no rules",
			rules: nil,
			obj:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		},
		{
			name:  "Node matches multiple rules",
			delay: 2 * time.Second,
			rules: []*readinessv1alpha1.NodeReadinessRule{
				{ObjectMeta: metav1.ObjectMeta{Name: "rule-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "rule-2"}},
			},
			obj: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "rule-1"}},
				{NamespacedName: types.NamespacedName{Name: "rule-2"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			wq := &recordingQueue{
				Queue: &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()},
			}
			h := NodeToRuleEventHandler{
				delay: tt.delay,
				getApplicableRulesForNode: func(context.Context, *corev1.Node) []*readinessv1alpha1.NodeReadinessRule {
					return tt.rules
				},
			}

			h.enqueue(t.Context(), tt.obj, wq)

			g.Expect(wq.Len()).To(Equal(len(tt.want)))

			var got []reconcile.Request
			for wq.Len() > 0 {
				item, _ := wq.Get()
				got = append(got, item)
			}
			g.Expect(got).To(ConsistOf(tt.want))

			for _, d := range wq.delays {
				g.Expect(d).To(Equal(tt.delay))
			}
		})
	}
}

func TestNodeToRuleEventHandler_Events(t *testing.T) {
	nodeNew := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-new"}}
	nodeOld := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-old"}}

	tests := []struct {
		name string
		call func(context.Context, NodeToRuleEventHandler, workqueue.TypedRateLimitingInterface[reconcile.Request])
		want []reconcile.Request
	}{
		{
			name: "Create event adds request to queue",
			call: func(ctx context.Context, h NodeToRuleEventHandler, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				h.Create(ctx, event.CreateEvent{Object: nodeNew}, q)
			},
			want: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "rule-node-new"}}},
		},
		{
			name: "Update event adds request to queue",
			call: func(ctx context.Context, h NodeToRuleEventHandler, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				h.Update(ctx, event.UpdateEvent{ObjectNew: nodeNew, ObjectOld: nodeOld}, q)
			},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "rule-node-new"}},
				{NamespacedName: types.NamespacedName{Name: "rule-node-old"}},
			},
		},
		{
			name: "Delete event adds request to queue",
			call: func(ctx context.Context, h NodeToRuleEventHandler, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				h.Delete(ctx, event.DeleteEvent{Object: nodeNew}, q)
			},
			want: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "rule-node-new"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			wq := &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()}
			h := NodeToRuleEventHandler{
				getApplicableRulesForNode: func(_ context.Context, n *corev1.Node) []*readinessv1alpha1.NodeReadinessRule {
					return []*readinessv1alpha1.NodeReadinessRule{
						{ObjectMeta: metav1.ObjectMeta{Name: "rule-" + n.Name}},
					}
				},
			}

			tt.call(t.Context(), h, wq)

			g.Expect(wq.Len()).To(Equal(len(tt.want)))

			var got []reconcile.Request
			for wq.Len() > 0 {
				item, _ := wq.Get()
				got = append(got, item)
			}
			g.Expect(got).To(ConsistOf(tt.want))
		})
	}
}
