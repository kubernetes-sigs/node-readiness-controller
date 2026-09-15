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
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	podsv1alpha1 "k8s.io/kubelet/pkg/apis/pods/v1alpha1"
)

// DefaultKubeletPodsAPISocket is the verified default socket path for the Kubelet
// PodsAPI (KEP-4188, feature gate PodsAPI, alpha in 1.36). The kubelet serves it at
// <root>/pods-api/pods-api.sock; default root is /var/lib/kubelet.
const DefaultKubeletPodsAPISocket = "/var/lib/kubelet/pods-api/pods-api.sock"

// KubeletPodSource is the real PodSource backed by the Kubelet PodsAPI WatchPods
// stream. It dials the node-local unix socket (filesystem-permission secured, so
// insecure transport credentials are correct) and translates WatchPodsEvents into
// the daemon's PodEvents.
//
// On each Watch call the kubelet replays an ADDED burst for existing pods followed
// by INITIAL_SYNC_COMPLETE, so reconnect handling is automatic: when the stream
// ends, Daemon.Run resets the cache and calls Watch again, and the fresh sync
// re-gates condition assertions.
type KubeletPodSource struct {
	socket string
}

// NewKubeletPodSource returns a PodSource for the given socket path (empty uses the
// default). The path is a "unix://" target for grpc.
func NewKubeletPodSource(socket string) *KubeletPodSource {
	if socket == "" {
		socket = DefaultKubeletPodsAPISocket
	}
	return &KubeletPodSource{socket: socket}
}

// Watch dials the socket, opens a WatchPods stream, and returns a channel of
// translated events that is closed when the stream ends (ctx cancelled or error).
func (s *KubeletPodSource) Watch(ctx context.Context) (<-chan PodEvent, error) {
	target := "unix://" + s.socket
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial kubelet PodsAPI at %s: %w", target, err)
	}

	client := podsv1alpha1.NewPodsClient(conn)
	stream, err := client.WatchPods(ctx, &podsv1alpha1.WatchPodsRequest{})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open WatchPods stream: %w", err)
	}

	out := make(chan PodEvent)
	go func() {
		defer close(out)
		defer func() { _ = conn.Close() }()
		log := klog.FromContext(ctx)
		for {
			ev, err := stream.Recv()
			if err != nil {
				// ctx cancellation is expected on shutdown; anything else is a
				// stream error that Daemon.Run handles by reconnecting.
				if ctx.Err() == nil {
					log.Error(err, "WatchPods stream ended; will reconnect")
				}
				return
			}
			pe, ok := translateEvent(ctx, ev)
			if !ok {
				continue
			}
			select {
			case out <- pe:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// translateEvent converts a kubelet WatchPodsEvent into a daemon PodEvent. It
// returns ok=false for events that should be skipped (UNSPECIFIED, or pod bytes
// that fail to decode).
func translateEvent(ctx context.Context, ev *podsv1alpha1.WatchPodsEvent) (PodEvent, bool) {
	log := klog.FromContext(ctx)

	switch ev.GetType() {
	case podsv1alpha1.EventType_INITIAL_SYNC_COMPLETE:
		// Pod bytes are nil for this event.
		return PodEvent{Type: EventInitialSyncComplete}, true
	case podsv1alpha1.EventType_ADDED, podsv1alpha1.EventType_MODIFIED, podsv1alpha1.EventType_DELETED:
		pod := &corev1.Pod{}
		if err := pod.Unmarshal(ev.GetPod()); err != nil {
			// For DELETED only name/namespace/UID are guaranteed set, but the bytes
			// still decode; a hard decode error means we skip this event.
			log.Error(err, "failed to unmarshal pod bytes from PodsAPI", "eventType", ev.GetType())
			return PodEvent{}, false
		}
		et, ok := mapEventType(ev.GetType())
		if !ok {
			return PodEvent{}, false
		}
		return PodEvent{Type: et, Pod: pod}, true
	case podsv1alpha1.EventType_UNSPECIFIED:
		log.V(4).Info("skipping unspecified PodsAPI event", "type", ev.GetType())
		return PodEvent{}, false
	default: // an enum value this build does not know
		log.Error(nil, "unknown PodsAPI event type; skipping event", "type", ev.GetType())
		return PodEvent{}, false
	}
}

// mapEventType converts the proto enum to the daemon's EventType. ok=false for
// an unmapped value: silently mislabeling an unknown event as MODIFIED would be
// a data-corruption footgun if the upstream enum ever grows, so it is logged
// and skipped instead.
func mapEventType(t podsv1alpha1.EventType) (EventType, bool) {
	switch t {
	case podsv1alpha1.EventType_ADDED:
		return EventAdded, true
	case podsv1alpha1.EventType_MODIFIED:
		return EventModified, true
	case podsv1alpha1.EventType_DELETED:
		return EventDeleted, true
	default:
		klog.ErrorS(nil, "unmapped PodsAPI event type; skipping event", "type", t)
		return EventModified, false
	}
}
