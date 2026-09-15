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

// Command node-readiness-daemon is the Node Readiness Daemon (NRD): a node-local
// component that watches pod health and publishes Node.status.conditions consumed
// by the Node Readiness Controller.
//
//	--source=fake   : replay a scripted scenario (no cluster needed; good for demos).
//	--source=kubelet: the real Kubelet PodsAPI (KEP-4188) WatchPods stream. Verified
//	                  end-to-end against kindest/node:v1.36.1.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	"sigs.k8s.io/node-readiness-controller/internal/daemon"
)

func main() {
	klog.InitFlags(nil)
	err := run()
	klog.Flush()
	if err != nil {
		os.Exit(1)
	}
}

// run holds main's body so deferred cleanups execute before the process exits
// (os.Exit in main would skip them).
func run() error {
	var (
		nodeName        = flag.String("node-name", os.Getenv("NODE_NAME"), "Name of the node this daemon runs on (or NODE_NAME env).")
		configDir       = flag.String("config-dir", "/etc/node-readiness/conditions.d", "Directory of drop-in condition configs.")
		source          = flag.String("source", "fake", "Pod source: 'fake' (scripted demo) or 'kubelet' (real PodsAPI WatchPods stream).")
		kubeletSocket   = flag.String("kubelet-socket", "/var/lib/kubelet/pods-api/pods-api.sock", "Kubelet PodsAPI unix socket (verified against v1.36.1; KEP-4188, feature gate PodsAPI).")
		kubeconfig      = flag.String("kubeconfig", "", "Path to kubeconfig (out-of-cluster). Empty uses in-cluster config.")
		impersonateNode = flag.Bool("impersonate-node", false, "Write conditions as system:node:<node-name> (NodeRestriction-scoped).")
		reconcile       = flag.Duration("reconcile-interval", daemon.DefaultReconcileInterval, "Periodic reconcile interval.")
		resync          = flag.Duration("resync-interval", daemon.DefaultResyncInterval, "Slow resync interval: force-reapply owned conditions even if unchanged (repairs external edits).")
		dryRun          = flag.Bool("dry-run", false, "Evaluate and log desired conditions without writing to the API server.")
	)
	flag.Parse()

	if *nodeName == "" {
		err := fmt.Errorf("node-name is required (set --node-name or NODE_NAME)")
		klog.ErrorS(err, "invalid flags")
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Build the pod source.
	var podSource daemon.PodSource
	switch *source {
	case "fake":
		klog.InfoS("PROTOTYPE: using FAKE pod source (scripted scenario); no real kubelet reads")
		podSource = demoSource(*nodeName)
	case "kubelet":
		klog.InfoS("using real kubelet PodsAPI source (KEP-4188)", "socket", *kubeletSocket)
		podSource = daemon.NewKubeletPodSource(*kubeletSocket)
	default:
		err := fmt.Errorf("unknown --source %q", *source)
		klog.ErrorS(err, "invalid flags")
		return err
	}

	// Build the condition writer.
	var writer daemon.ConditionWriter
	if *dryRun {
		klog.InfoS("dry-run: conditions will be logged, not written")
		writer = &logWriter{}
	} else {
		client, err := buildClient(*kubeconfig, *impersonateNode, *nodeName)
		if err != nil {
			klog.ErrorS(err, "failed to build kubernetes client")
			return err
		}
		writer = daemon.NewNodeConditionWriter(client, *nodeName, clock.RealClock{})
	}

	d := daemon.New(daemon.Options{
		Source:            podSource,
		Store:             daemon.NewConfigStore(*configDir),
		Writer:            writer,
		Clock:             clock.RealClock{},
		ReconcileInterval: *reconcile,
		ResyncInterval:    *resync,
	})

	klog.InfoS("starting node-readiness-daemon", "node", *nodeName, "configDir", *configDir, "source", *source)
	if err := d.Run(ctx); err != nil && ctx.Err() == nil {
		klog.ErrorS(err, "daemon exited with error")
		return err
	}
	klog.InfoS("node-readiness-daemon stopped")
	return nil
}

func buildClient(kubeconfig string, impersonate bool, nodeName string) (kubernetes.Interface, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	if impersonate {
		cfg.Impersonate = rest.ImpersonationConfig{UserName: "system:node:" + nodeName}
		klog.InfoS("impersonating node identity for writes", "as", cfg.Impersonate.UserName)
	}
	return kubernetes.NewForConfig(cfg)
}

// demoSource scripts a small scenario: a critical pod appears not-ready, then
// becomes ready after the initial sync — so you can watch a condition flip.
func demoSource(_ string) daemon.PodSource {
	notReady := buildPod("kube-system", "cilium-xyz", map[string]string{"k8s-app": "cilium"}, false)
	ready := buildPod("kube-system", "cilium-xyz", map[string]string{"k8s-app": "cilium"}, true)
	return &daemon.FakePodSource{Script: []daemon.PodEvent{
		{Type: daemon.EventAdded, Pod: notReady},
		{Type: daemon.EventInitialSyncComplete},
		{Type: daemon.EventModified, Pod: ready},
	}}
}

func buildPod(ns, name string, labels map[string]string, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, UID: types.UID(ns + "/" + name)},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}},
		},
	}
}

// logWriter is a dry-run ConditionWriter that logs instead of writing.
type logWriter struct{}

func (l *logWriter) Sync(_ context.Context, desired []daemon.DesiredCondition, pending sets.Set[string]) error {
	for _, d := range desired {
		klog.InfoS("[dry-run] would set node condition",
			"type", d.Type, "status", d.Status, "reason", d.Reason, "message", d.Message)
	}
	for t := range pending {
		klog.InfoS("[dry-run] condition pending (would keep any published value)", "type", t)
	}
	return nil
}
