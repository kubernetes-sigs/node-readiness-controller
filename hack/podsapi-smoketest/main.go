// Smoketest probe: exercises the real Kubelet PodsAPI (KEP-4188, alpha 1.36)
// against the live socket inside a node. Confirms payload format, event types,
// and that ListPods/GetPod/WatchPods api work as expected.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	podsv1alpha1 "k8s.io/kubelet/pkg/apis/pods/v1alpha1"
)

const socket = "unix:///var/lib/kubelet/pods-api/pods-api.sock"

// outf/outln print probe output to stdout; write errors are ignored by design
// (this is a throwaway diagnostic tool).
func outf(format string, args ...any) { _, _ = fmt.Fprintf(os.Stdout, format, args...) }
func outln(args ...any)               { _, _ = fmt.Fprintln(os.Stdout, args...) }

func podSummary(b []byte) string {
	if len(b) == 0 {
		return "<no pod bytes>"
	}
	var p corev1.Pod
	if err := p.Unmarshal(b); err != nil {
		return fmt.Sprintf("<unmarshal err: %v>", err)
	}
	ready := "?"
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			ready = string(c.Status)
		}
	}
	return fmt.Sprintf("%s/%s phase=%s ready=%s uid=%s", p.Namespace, p.Name, p.Status.Phase, ready, p.UID)
}

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run holds main's body so the deferred conn.Close/cancel execute before exit.
func run() error {
	conn, err := grpc.NewClient(socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		outf("DIAL ERROR: %v\n", err)
		return err
	}
	defer func() { _ = conn.Close() }()
	client := podsv1alpha1.NewPodsClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. ListPods (request is empty — no fieldmask/pagination/filter fields exist).
	lp, err := client.ListPods(ctx, &podsv1alpha1.ListPodsRequest{})
	if err != nil {
		outf("ListPods ERROR: %v\n", err)
	} else {
		outf("=== ListPods: %d pods ===\n", len(lp.Pods))
		for _, b := range lp.Pods {
			outf("  %s\n", podSummary(b))
		}
	}

	// 2. GetPod by UID (only field in the request is podUID).
	if lp != nil && len(lp.Pods) > 0 {
		var first corev1.Pod
		_ = first.Unmarshal(lp.Pods[0])
		gp, err := client.GetPod(ctx, &podsv1alpha1.GetPodRequest{PodUID: string(first.UID)})
		if err != nil {
			outf("GetPod ERROR: %v\n", err)
		} else {
			outf("=== GetPod(%s): %s ===\n", first.UID, podSummary(gp.Pod))
		}
	}

	// 3. WatchPods: initial ADDED burst + INITIAL_SYNC_COMPLETE, then live events.
	stream, err := client.WatchPods(ctx, &podsv1alpha1.WatchPodsRequest{})
	if err != nil {
		outf("WatchPods ERROR: %v\n", err)
		return err
	}
	outf("=== WatchPods stream ===\n")
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			outln("(stream EOF)")
			return nil
		}
		if err != nil {
			// context deadline is the expected end of this probe.
			outf("(stream end: %v)\n", err)
			return nil
		}
		switch ev.Type {
		case podsv1alpha1.EventType_INITIAL_SYNC_COMPLETE:
			outln("  [INITIAL_SYNC_COMPLETE] (pod is nil here:", len(ev.Pod) == 0, ")")
		default:
			outf("  [%s] %s\n", ev.Type, podSummary(ev.Pod))
		}
	}
}
