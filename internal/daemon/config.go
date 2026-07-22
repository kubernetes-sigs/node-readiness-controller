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

// Package daemon implements the Node Readiness Daemon (NRD): a node-local
// component that watches pod health (via the Kubelet PodInfoAPI, KEP-4188) and
// publishes Node.status.conditions consumed by the Node Readiness Controller.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

const (
	defaultMinPods            = 1
	defaultHealthyThreshold   = 30 * time.Second
	defaultUnhealthyThreshold = 10 * time.Second
)

// Config is a validated, in-memory readiness-condition definition (one per file).
type Config struct {
	// ConditionType is the unique, prefixed Node condition this config owns.
	ConditionType string
	// Namespace optionally narrows pod selection. Empty means all namespaces.
	Namespace string
	// Selector matches the critical pods for this condition.
	Selector labels.Selector
	// MinPods is the fail-closed floor: synced + fewer ready pods than this => False.
	MinPods int
	// HealthyThreshold is the sustained-healthy duration before asserting True.
	HealthyThreshold time.Duration
	// UnhealthyThreshold is the sustained-unhealthy duration before asserting False.
	UnhealthyThreshold time.Duration
	// SourceFile is the originating file path (diagnostics only).
	SourceFile string
}

// configFile is the on-disk YAML schema (parsed via sigs.k8s.io/yaml, JSON tags).
type configFile struct {
	ConditionType string `json:"conditionType"`
	Selector      struct {
		Namespace   string            `json:"namespace"`
		MatchLabels map[string]string `json:"matchLabels"`
	} `json:"selector"`
	MinPods  *int `json:"minPods"`
	Debounce struct {
		HealthyThreshold   string `json:"healthyThreshold"`
		UnhealthyThreshold string `json:"unhealthyThreshold"`
	} `json:"debounce"`
}

// parseConfig parses and validates a single config file's bytes.
func parseConfig(path string, data []byte) (Config, error) {
	var cf configFile
	if err := yaml.UnmarshalStrict(data, &cf); err != nil {
		return Config{}, fmt.Errorf("parse: %w", err)
	}

	if cf.ConditionType == "" {
		return Config{}, fmt.Errorf("conditionType is required")
	}
	if err := validateConditionType(cf.ConditionType); err != nil {
		return Config{}, err
	}
	if len(cf.Selector.MatchLabels) == 0 {
		return Config{}, fmt.Errorf("selector.matchLabels is required (an empty selector would match every pod)")
	}

	sel := labels.SelectorFromSet(labels.Set(cf.Selector.MatchLabels))

	minPods := defaultMinPods
	if cf.MinPods != nil {
		minPods = *cf.MinPods
	}
	// Fail-closed: minPods must be >= 1. minPods: 0 would make an empty match
	// vacuously healthy, which is exactly the not-yet-scheduled hole we close.
	if minPods < 1 {
		return Config{}, fmt.Errorf("minPods must be >= 1 (got %d); 0 is fail-open and disallowed", minPods)
	}

	healthy, err := parseThreshold(cf.Debounce.HealthyThreshold, defaultHealthyThreshold)
	if err != nil {
		return Config{}, fmt.Errorf("debounce.healthyThreshold: %w", err)
	}
	unhealthy, err := parseThreshold(cf.Debounce.UnhealthyThreshold, defaultUnhealthyThreshold)
	if err != nil {
		return Config{}, fmt.Errorf("debounce.unhealthyThreshold: %w", err)
	}

	return Config{
		ConditionType:      cf.ConditionType,
		Namespace:          cf.Selector.Namespace,
		Selector:           sel,
		MinPods:            minPods,
		HealthyThreshold:   healthy,
		UnhealthyThreshold: unhealthy,
		SourceFile:         path,
	}, nil
}

// validateConditionType requires a domain-qualified condition type
// ("<domain>/<Name>", e.g. "cni.example.io/NetworkReady"): a DNS-subdomain-ish
// domain (must contain a "." so it cannot be mistaken for a bare word) and a
// CamelCase name. This intentionally excludes every kubelet built-in condition
// (Ready, MemoryPressure, ...), which are all unqualified, so the daemon can
// never collide with — or SSA-claim ownership of — kubelet's own conditions.
func validateConditionType(t string) error {
	slash := strings.Index(t, "/")
	if slash < 0 {
		return fmt.Errorf("conditionType %q must be domain-qualified (\"<domain>/<Name>\", e.g. \"cni.example.io/NetworkReady\")", t)
	}
	domain, name := t[:slash], t[slash+1:]
	if domain == "" || !strings.Contains(domain, ".") || strings.ContainsAny(domain, " \t/") {
		return fmt.Errorf("conditionType %q: domain %q must be a DNS subdomain (non-empty, containing a \".\", no spaces)", t, domain)
	}
	if name == "" || strings.ContainsAny(name, " \t/") || name[0] < 'A' || name[0] > 'Z' {
		return fmt.Errorf("conditionType %q: name %q must be non-empty CamelCase (start with an uppercase letter, no spaces)", t, name)
	}
	return nil
}

func parseThreshold(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return d, nil
}

// ConfigStore loads and reloads conditions.d/*.yaml. On reload it keeps the
// last-good version of a file whose new contents fail to parse, so a fat-finger
// edit cannot silently drop a condition (and un-taint nodes). Removing the file
// removes the condition.
type ConfigStore struct {
	dir    string
	byFile map[string]Config
	// meta records size+mtime of each successfully parsed file so Reload can
	// skip re-reading and re-parsing unchanged files. Recorded only on a
	// successful parse, so a broken file keeps being retried (and its error
	// keeps being surfaced) on every reload.
	meta map[string]fileMeta
}

// fileMeta is the change-detection fingerprint for a parsed config file.
type fileMeta struct {
	size    int64
	modTime time.Time
}

// NewConfigStore creates a store rooted at dir.
func NewConfigStore(dir string) *ConfigStore {
	return &ConfigStore{dir: dir, byFile: map[string]Config{}, meta: map[string]fileMeta{}}
}

// Reload rescans the directory and returns the current valid config set plus any
// non-fatal errors encountered (invalid files that were skipped). Duplicate
// conditionType across files is rejected (the later file, by sorted path, loses).
func (s *ConfigStore) Reload() ([]Config, []error) {
	var errs []error

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		// A missing/unreadable dir yields zero configs (fail-closed: nothing asserted).
		return nil, []error{fmt.Errorf("read config dir %q: %w", s.dir, err)}
	}

	seenFiles := map[string]bool{}
	var paths []string
	metaByPath := map[string]fileMeta{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(e.Name())); ext != ".yaml" && ext != ".yml" {
			continue
		}
		p := filepath.Join(s.dir, e.Name())
		paths = append(paths, p)
		if info, err := e.Info(); err == nil {
			metaByPath[p] = fileMeta{size: info.Size(), modTime: info.ModTime()}
		}
	}
	sort.Strings(paths) // deterministic duplicate-resolution order

	for _, p := range paths {
		seenFiles[p] = true
		m, haveMeta := metaByPath[p]
		// Unchanged size+mtime and a previously parsed Config: skip the
		// read+parse — reloads run on every periodic tick and the files change
		// rarely.
		if haveMeta {
			if prev, ok := s.meta[p]; ok && prev == m {
				if _, parsed := s.byFile[p]; parsed {
					continue
				}
			}
		}
		data, err := os.ReadFile(p) //nolint:gosec // p is confined to the operator-controlled config dir
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue // keep last-good (handled below by not deleting)
		}
		cfg, err := parseConfig(p, data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue // keep last-good
		}
		s.byFile[p] = cfg
		if haveMeta {
			s.meta[p] = m
		}
	}

	// Drop files that no longer exist on disk.
	for p := range s.byFile {
		if !seenFiles[p] {
			delete(s.byFile, p)
			delete(s.meta, p)
		}
	}

	// Resolve duplicate conditionType deterministically (first sorted path wins).
	out := make([]Config, 0, len(s.byFile))
	ownedType := map[string]string{} // conditionType -> winning file
	for _, p := range paths {
		cfg, ok := s.byFile[p]
		if !ok {
			continue
		}
		if winner, dup := ownedType[cfg.ConditionType]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate conditionType %q already declared by %s; skipping", p, cfg.ConditionType, winner))
			continue
		}
		ownedType[cfg.ConditionType] = p
		out = append(out, cfg)
	}
	return out, errs
}
