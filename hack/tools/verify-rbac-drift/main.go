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

// verify-rbac-drift compares the RBAC the Helm chart renders against the RBAC
// in config/rbac, which is the source of truth because role.yaml is generated
// by controller-gen from the kubebuilder markers on the controller.
//
// The two cannot be compared textually: the generated roles carry short names
// and kustomize labels, while the chart templates the name from the release and
// adds Helm labels. So each Role and ClusterRole is matched by name, ignoring
// the chart's release prefix, and only the rules are compared. Rules are
// normalised first, since ordering within a rule carries no meaning to the API
// server and should not be treated as drift.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// rbacDoc is the subset of a Role or ClusterRole this check cares about.
type rbacDoc struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Rules []rbacv1.PolicyRule `json:"rules"`
}

// role is a Role or ClusterRole keyed by its unprefixed name.
type role struct {
	kind  string
	name  string
	rules []rbacv1.PolicyRule
}

func main() {
	var (
		chartManifest string
		configDir     string
		prefix        string
	)

	flag.StringVar(&chartManifest, "chart-manifest", "", "path to the rendered Helm manifest, or - to read stdin")
	flag.StringVar(&configDir, "config-dir", "config/rbac", "directory holding the source of truth RBAC manifests")
	flag.StringVar(&prefix, "prefix", "", "release name prefix to strip from rendered chart resource names")
	flag.Parse()

	if chartManifest == "" {
		fmt.Fprintln(os.Stderr, "error: -chart-manifest is required")
		os.Exit(2)
	}

	want, err := loadDir(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: reading %s: %v\n", configDir, err)
		os.Exit(2)
	}
	if len(want) == 0 {
		fmt.Fprintf(os.Stderr, "error: no Role or ClusterRole found in %s\n", configDir)
		os.Exit(2)
	}

	got, err := loadManifest(chartManifest, prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: reading %s: %v\n", chartManifest, err)
		os.Exit(2)
	}

	if drift := compare(want, got); len(drift) > 0 {
		fmt.Fprintf(os.Stderr, "RBAC drift between %s and the Helm chart:\n\n", configDir)
		for _, d := range drift {
			fmt.Fprintf(os.Stderr, "%s\n", d)
		}
		fmt.Fprintln(os.Stderr, "The chart grants different permissions than the controller declares.")
		fmt.Fprintf(os.Stderr, "Update charts/nrr-controller/templates/rbac.yaml to match %s.\n", configDir)
		os.Exit(1)
	}

	_, _ = fmt.Fprintf(os.Stdout, "RBAC in the Helm chart matches %s (%d roles compared)\n", configDir, len(want))
}

// compare reports every role whose rules differ, or that the chart never renders.
func compare(want, got map[string]role) []string {
	var drift []string

	for _, name := range sortedKeys(want) {
		expected := want[name]

		actual, ok := got[name]
		if !ok {
			drift = append(drift, fmt.Sprintf("%s %q is missing from the chart\n", expected.kind, name))
			continue
		}

		expectedRules := normalize(expected.rules)
		actualRules := normalize(actual.rules)
		if reflect.DeepEqual(expectedRules, actualRules) {
			continue
		}

		drift = append(drift, fmt.Sprintf("%s %q has different rules\n%s", expected.kind, name,
			diffRules(expectedRules, actualRules)))
	}

	// A role the chart adds on its own is worth surfacing, but it is not drift:
	// the chart may legitimately ship something kustomize does not.
	for _, name := range sortedKeys(got) {
		if _, ok := want[name]; !ok {
			_, _ = fmt.Fprintf(os.Stdout, "note: %s %q is rendered by the chart but has no counterpart in config\n", got[name].kind, name)
		}
	}

	return drift
}

// diffRules renders the rules only one side has, so the output points at the
// exact permission that drifted rather than dumping both role definitions.
func diffRules(want, got []rbacv1.PolicyRule) string {
	var b strings.Builder

	for _, rule := range want {
		if !containsRule(got, rule) {
			fmt.Fprintf(&b, "  only in config: %s\n", formatRule(rule))
		}
	}
	for _, rule := range got {
		if !containsRule(want, rule) {
			fmt.Fprintf(&b, "  only in chart:  %s\n", formatRule(rule))
		}
	}

	return b.String()
}

func containsRule(rules []rbacv1.PolicyRule, target rbacv1.PolicyRule) bool {
	for _, rule := range rules {
		if reflect.DeepEqual(rule, target) {
			return true
		}
	}
	return false
}

func formatRule(rule rbacv1.PolicyRule) string {
	var parts []string
	if len(rule.APIGroups) > 0 {
		parts = append(parts, "apiGroups="+formatList(rule.APIGroups))
	}
	if len(rule.Resources) > 0 {
		parts = append(parts, "resources="+formatList(rule.Resources))
	}
	if len(rule.ResourceNames) > 0 {
		parts = append(parts, "resourceNames="+formatList(rule.ResourceNames))
	}
	if len(rule.NonResourceURLs) > 0 {
		parts = append(parts, "nonResourceURLs="+formatList(rule.NonResourceURLs))
	}
	parts = append(parts, "verbs="+formatList(rule.Verbs))
	return strings.Join(parts, " ")
}

// formatList renders the empty string as "" so the core API group stays visible.
func formatList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			quoted = append(quoted, `""`)
			continue
		}
		quoted = append(quoted, v)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// normalize sorts every list in every rule and then the rules themselves, so
// that only a real difference in granted permissions counts as drift.
func normalize(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	out := make([]rbacv1.PolicyRule, 0, len(rules))

	for _, rule := range rules {
		rule.APIGroups = sortedCopy(rule.APIGroups)
		rule.Resources = sortedCopy(rule.Resources)
		rule.Verbs = sortedCopy(rule.Verbs)
		rule.ResourceNames = sortedCopy(rule.ResourceNames)
		rule.NonResourceURLs = sortedCopy(rule.NonResourceURLs)
		out = append(out, rule)
	}

	sort.Slice(out, func(i, j int) bool {
		return formatRule(out[i]) < formatRule(out[j])
	})

	return out
}

func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]role) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// loadDir collects every Role and ClusterRole defined under dir.
func loadDir(dir string) (map[string]role, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	roles := make(map[string]role)
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) {
			continue
		}

		found, err := decodeFile(filepath.Join(dir, entry.Name()), "")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		for name, r := range found {
			roles[name] = r
		}
	}

	return roles, nil
}

// loadManifest collects every Role and ClusterRole in a rendered manifest,
// stripping prefix from the resource names so they line up with config.
func loadManifest(path, prefix string) (map[string]role, error) {
	if path == "-" {
		return decode(os.Stdin, prefix)
	}
	return decodeFile(path, prefix)
}

func decodeFile(path, prefix string) (map[string]role, error) {
	f, err := os.Open(path) //nolint:gosec // paths come from the repo layout, not user input
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	return decode(f, prefix)
}

func decode(r io.Reader, prefix string) (map[string]role, error) {
	roles := make(map[string]role)
	decoder := utilyaml.NewYAMLOrJSONDecoder(r, 4096)

	for {
		var doc rbacDoc
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				return roles, nil
			}
			return nil, err
		}

		if doc.Kind != "Role" && doc.Kind != "ClusterRole" {
			continue
		}

		name := strings.TrimPrefix(doc.Metadata.Name, prefix)
		roles[name] = role{kind: doc.Kind, name: name, rules: doc.Rules}
	}
}

func isYAML(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}
