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
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(t *testing.T, c Config)
	}{
		{
			name: "valid full config",
			yaml: `
conditionType: readiness.k8s.io/CNIReady
selector:
  namespace: kube-system
  matchLabels:
    k8s-app: cilium
minPods: 2
debounce:
  healthyThreshold: 45s
  unhealthyThreshold: 5s
`,
			check: func(t *testing.T, c Config) {
				t.Helper()
				if c.ConditionType != "readiness.k8s.io/CNIReady" {
					t.Errorf("conditionType = %q", c.ConditionType)
				}
				if c.Namespace != "kube-system" {
					t.Errorf("namespace = %q", c.Namespace)
				}
				if c.MinPods != 2 {
					t.Errorf("minPods = %d, want 2", c.MinPods)
				}
				if c.HealthyThreshold != 45*time.Second || c.UnhealthyThreshold != 5*time.Second {
					t.Errorf("thresholds = %v/%v", c.HealthyThreshold, c.UnhealthyThreshold)
				}
			},
		},
		{
			name: "defaults applied",
			yaml: `
conditionType: readiness.k8s.io/X
selector:
  matchLabels: {app: foo}
`,
			check: func(t *testing.T, c Config) {
				t.Helper()
				if c.MinPods != defaultMinPods {
					t.Errorf("minPods = %d, want default %d", c.MinPods, defaultMinPods)
				}
				if c.HealthyThreshold != defaultHealthyThreshold || c.UnhealthyThreshold != defaultUnhealthyThreshold {
					t.Errorf("default thresholds not applied: %v/%v", c.HealthyThreshold, c.UnhealthyThreshold)
				}
			},
		},
		{
			name:    "missing conditionType",
			yaml:    "selector:\n  matchLabels: {app: foo}\n",
			wantErr: true,
		},
		{
			name: "domain-qualified conditionType accepted",
			yaml: "conditionType: cilium.io/CNINetworkReady\nselector:\n  matchLabels: {app: foo}\n",
			check: func(t *testing.T, c Config) {
				t.Helper()
				if c.ConditionType != "cilium.io/CNINetworkReady" {
					t.Errorf("conditionType = %q", c.ConditionType)
				}
			},
		},
		{
			name:    "unqualified conditionType rejected (kubelet built-in shape)",
			yaml:    "conditionType: CNIReady\nselector:\n  matchLabels: {app: foo}\n",
			wantErr: true,
		},
		{
			name:    "kubelet built-in Ready rejected",
			yaml:    "conditionType: Ready\nselector:\n  matchLabels: {app: foo}\n",
			wantErr: true,
		},
		{
			name:    "dotless domain rejected",
			yaml:    "conditionType: foo/Bar\nselector:\n  matchLabels: {app: foo}\n",
			wantErr: true,
		},
		{
			name:    "lowercase condition name rejected",
			yaml:    "conditionType: example.com/ready\nselector:\n  matchLabels: {app: foo}\n",
			wantErr: true,
		},
		{
			name:    "empty condition name rejected",
			yaml:    "conditionType: example.com/\nselector:\n  matchLabels: {app: foo}\n",
			wantErr: true,
		},
		{
			name:    "empty selector rejected (would match everything)",
			yaml:    "conditionType: readiness.k8s.io/X\nselector:\n  matchLabels: {}\n",
			wantErr: true,
		},
		{
			name:    "minPods zero is fail-open and rejected",
			yaml:    "conditionType: readiness.k8s.io/X\nselector:\n  matchLabels: {app: foo}\nminPods: 0\n",
			wantErr: true,
		},
		{
			name:    "negative minPods rejected",
			yaml:    "conditionType: readiness.k8s.io/X\nselector:\n  matchLabels: {app: foo}\nminPods: -1\n",
			wantErr: true,
		},
		{
			name:    "unknown field rejected (strict)",
			yaml:    "conditionType: readiness.k8s.io/X\nselector:\n  matchLabels: {app: foo}\nbogusField: true\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseConfig("test.yaml", []byte(tt.yaml))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, c)
			}
		})
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestConfigStoreReload(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir)

	const cniGood = "conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\n"
	writeFile(t, dir, "cni.yaml", cniGood)
	writeFile(t, dir, "gpu.yaml", "conditionType: readiness.k8s.io/GPUReady\nselector:\n  matchLabels: {k8s-app: nvidia}\n")
	writeFile(t, dir, "ignored.txt", "not yaml")

	cfgs, errs := store.Reload()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(cfgs) != 2 {
		t.Fatalf("got %d configs, want 2", len(cfgs))
	}

	// Invalid edit to a previously-good file: keep last-good, report error.
	writeFile(t, dir, "cni.yaml", "conditionType: \nbroken: [")
	cfgs, errs = store.Reload()
	if len(errs) == 0 {
		t.Errorf("expected a parse error for broken cni.yaml")
	}
	if len(cfgs) != 2 {
		t.Errorf("got %d configs, want 2 (last-good cni retained)", len(cfgs))
	}
	if !hasConfigType(cfgs, "readiness.k8s.io/CNIReady") {
		t.Errorf("CNIReady dropped after invalid edit; should retain last-good")
	}

	// File removal drops the condition.
	if err := os.Remove(filepath.Join(dir, "cni.yaml")); err != nil {
		t.Fatal(err)
	}
	cfgs, _ = store.Reload()
	if hasConfigType(cfgs, "readiness.k8s.io/CNIReady") {
		t.Errorf("CNIReady should be gone after file removal")
	}
	if len(cfgs) != 1 {
		t.Errorf("got %d configs, want 1", len(cfgs))
	}
}

func TestConfigStoreDuplicateConditionType(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir)
	// Two files, same conditionType. Sorted-first wins; second is an error.
	writeFile(t, dir, "a.yaml", "conditionType: readiness.k8s.io/Dup\nselector:\n  matchLabels: {app: a}\n")
	writeFile(t, dir, "b.yaml", "conditionType: readiness.k8s.io/Dup\nselector:\n  matchLabels: {app: b}\n")

	cfgs, errs := store.Reload()
	if len(cfgs) != 1 {
		t.Fatalf("got %d configs, want 1 (dedup by conditionType)", len(cfgs))
	}
	if len(errs) == 0 {
		t.Errorf("expected a duplicate-conditionType error")
	}
	// a.yaml (sorted first) wins.
	if cfgs[0].SourceFile != filepath.Join(dir, "a.yaml") {
		t.Errorf("winner = %s, want a.yaml", cfgs[0].SourceFile)
	}
}

func hasConfigType(cfgs []Config, t string) bool {
	for _, c := range cfgs {
		if c.ConditionType == t {
			return true
		}
	}
	return false
}

// Reload must skip re-reading+re-parsing files whose size+mtime fingerprint is
// unchanged, and must re-parse when the fingerprint changes.
func TestConfigStoreReloadSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir)

	p := writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 30s\n")
	cfgs, errs := store.Reload()
	if len(errs) != 0 || len(cfgs) != 1 {
		t.Fatalf("setup reload: cfgs=%d errs=%v", len(cfgs), errs)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	// Same-size edit with the mtime restored: fingerprint unchanged, so the
	// file must NOT be re-parsed (the old 30s threshold is still served).
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 31s\n")
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	cfgs, _ = store.Reload()
	if got := cfgs[0].HealthyThreshold; got != 30*time.Second {
		t.Fatalf("threshold = %v after fingerprint-identical rewrite, want 30s (parse should have been skipped)", got)
	}

	// Bump the mtime: fingerprint changed, file re-parsed.
	if err := os.Chtimes(p, info.ModTime().Add(2*time.Second), info.ModTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	cfgs, _ = store.Reload()
	if got := cfgs[0].HealthyThreshold; got != 31*time.Second {
		t.Fatalf("threshold = %v after mtime bump, want 31s (re-parse expected)", got)
	}
}
