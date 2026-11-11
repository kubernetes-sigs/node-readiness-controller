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
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestNodeReadinessChanged_Update(t *testing.T) {
	now := metav1.Now()
	earlier := metav1.NewTime(now.Add(-10 * time.Minute))

	tests := []struct {
		name string
		e    event.UpdateEvent
		want bool
	}{
		{
			name: "ObjectOld is nil",
			e: event.UpdateEvent{
				ObjectNew: &corev1.Node{},
			},
			want: false,
		},
		{
			name: "ObjectNew is nil",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{},
			},
			want: false,
		},
		{
			name: "Both objects nil",
			e:    event.UpdateEvent{},
			want: false,
		},
		{
			name: "ObjectOld is not a Node",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Pod{},
				ObjectNew: &corev1.Node{},
			},
			want: false,
		},
		{
			name: "ObjectNew is not a Node",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{},
				ObjectNew: &corev1.Pod{},
			},
			want: false,
		},
		{
			name: "Unrelated node spec/status update (capacity or images changed)",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"zone": "us-east-1a"},
					},
					Spec: corev1.NodeSpec{
						PodCIDR: "10.244.0.0/24",
					},
				},
				ObjectNew: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"zone": "us-east-1a"},
					},
					Spec: corev1.NodeSpec{
						PodCIDR: "10.244.1.0/24",
					},
				},
			},
			want: false,
		},
		{
			name: "Routine kubelet heartbeat (only LastHeartbeatTime changed)",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "node-1",
						ResourceVersion: "100",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:               corev1.NodeReady,
								Status:             corev1.ConditionTrue,
								LastHeartbeatTime:  earlier,
								LastTransitionTime: earlier,
							},
						},
					},
				},
				ObjectNew: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "node-1",
						ResourceVersion: "101",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:               corev1.NodeReady,
								Status:             corev1.ConditionTrue,
								LastHeartbeatTime:  now,
								LastTransitionTime: earlier,
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Conditions changed - condition status changed",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
				ObjectNew: &corev1.Node{
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Conditions changed - condition added",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{},
				ObjectNew: &corev1.Node{
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Conditions changed - condition removed",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				ObjectNew: &corev1.Node{},
			},
			want: true,
		},
		{
			name: "Taints changed - taint added",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{},
				ObjectNew: &corev1.Node{
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{
								Key:    "readiness.k8s.io/gpu-ready",
								Value:  "true",
								Effect: corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Taints changed - taint removed",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{
								Key:    "readiness.k8s.io/gpu-ready",
								Value:  "true",
								Effect: corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
				ObjectNew: &corev1.Node{},
			},
			want: true,
		},
		{
			name: "Taints changed - taint effect changed",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{
								Key:    "readiness.k8s.io/gpu-ready",
								Effect: corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
				ObjectNew: &corev1.Node{
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{
								Key:    "readiness.k8s.io/gpu-ready",
								Effect: corev1.TaintEffectPreferNoSchedule,
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Taints changed - taint value changed",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{
								Key:    "readiness.k8s.io/gpu-ready",
								Value:  "v1",
								Effect: corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
				ObjectNew: &corev1.Node{
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{
								Key:    "readiness.k8s.io/gpu-ready",
								Value:  "v2",
								Effect: corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Labels changed - label added",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{},
				ObjectNew: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"gpu": "true"},
					},
				},
			},
			want: true,
		},
		{
			name: "Labels changed - label removed",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"gpu": "true"},
					},
				},
				ObjectNew: &corev1.Node{},
			},
			want: true,
		},
		{
			name: "Labels changed - label value changed",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"env": "staging"},
					},
				},
				ObjectNew: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"env": "prod"},
					},
				},
			},
			want: true,
		},
		{
			name: "Multiple attributes changed (conditions and labels)",
			e: event.UpdateEvent{
				ObjectOld: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"zone": "a"},
					},
				},
				ObjectNew: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"zone": "b"},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			p := NodeReadinessChanged{
				logger: logr.Discard(),
			}

			g.Expect(p.Update(tt.e)).To(Equal(tt.want))
		})
	}
}
