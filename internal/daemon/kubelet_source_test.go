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

package daemon

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	podsv1alpha1 "k8s.io/kubelet/pkg/apis/pods/v1alpha1"
)

// marshalPod encodes a pod the way the kubelet PodsAPI does (k8s protobuf).
func marshalPod(t *testing.T, p *corev1.Pod) []byte {
	t.Helper()
	b, err := p.Marshal()
	if err != nil {
		t.Fatalf("marshal pod: %v", err)
	}
	return b
}

func TestTranslateEvent(t *testing.T) {
	ctx := context.Background()
	p := pod("kube-system", "cilium-abc", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)
	podBytes := marshalPod(t, p)

	tests := []struct {
		name     string
		ev       *podsv1alpha1.WatchPodsEvent
		wantOK   bool
		wantType EventType
		wantPod  bool
	}{
		{
			name:     "initial sync complete has nil pod",
			ev:       &podsv1alpha1.WatchPodsEvent{Type: podsv1alpha1.EventType_INITIAL_SYNC_COMPLETE},
			wantOK:   true,
			wantType: EventInitialSyncComplete,
		},
		{
			name:     "added",
			ev:       &podsv1alpha1.WatchPodsEvent{Type: podsv1alpha1.EventType_ADDED, Pod: podBytes},
			wantOK:   true,
			wantType: EventAdded,
			wantPod:  true,
		},
		{
			name:     "modified",
			ev:       &podsv1alpha1.WatchPodsEvent{Type: podsv1alpha1.EventType_MODIFIED, Pod: podBytes},
			wantOK:   true,
			wantType: EventModified,
			wantPod:  true,
		},
		{
			name:     "deleted",
			ev:       &podsv1alpha1.WatchPodsEvent{Type: podsv1alpha1.EventType_DELETED, Pod: podBytes},
			wantOK:   true,
			wantType: EventDeleted,
			wantPod:  true,
		},
		{
			name:   "decode error is skipped",
			ev:     &podsv1alpha1.WatchPodsEvent{Type: podsv1alpha1.EventType_ADDED, Pod: []byte{0xff, 0x01, 0x02}},
			wantOK: false,
		},
		{
			name:   "unspecified is skipped",
			ev:     &podsv1alpha1.WatchPodsEvent{Type: podsv1alpha1.EventType_UNSPECIFIED},
			wantOK: false,
		},
		{
			name:   "unknown future enum value is skipped, not mislabeled",
			ev:     &podsv1alpha1.WatchPodsEvent{Type: podsv1alpha1.EventType(99)},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe, ok := translateEvent(ctx, tt.ev)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if pe.Type != tt.wantType {
				t.Errorf("type = %v, want %v", pe.Type, tt.wantType)
			}
			if tt.wantPod {
				if pe.Pod == nil {
					t.Fatalf("pod = nil, want decoded pod")
				}
				if pe.Pod.Name != p.Name || pe.Pod.Namespace != p.Namespace {
					t.Errorf("pod = %s/%s, want %s/%s", pe.Pod.Namespace, pe.Pod.Name, p.Namespace, p.Name)
				}
			} else if pe.Pod != nil {
				t.Errorf("pod = %v, want nil", pe.Pod)
			}
		})
	}
}

func TestMapEventType(t *testing.T) {
	tests := []struct {
		in     podsv1alpha1.EventType
		want   EventType
		wantOK bool
	}{
		{podsv1alpha1.EventType_ADDED, EventAdded, true},
		{podsv1alpha1.EventType_MODIFIED, EventModified, true},
		{podsv1alpha1.EventType_DELETED, EventDeleted, true},
		{podsv1alpha1.EventType_UNSPECIFIED, EventModified, false},
		{podsv1alpha1.EventType(42), EventModified, false},
	}
	for _, tt := range tests {
		got, ok := mapEventType(tt.in)
		if ok != tt.wantOK {
			t.Errorf("mapEventType(%v) ok = %v, want %v", tt.in, ok, tt.wantOK)
		}
		if ok && got != tt.want {
			t.Errorf("mapEventType(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
