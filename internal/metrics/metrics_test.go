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

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestBuildInfo(t *testing.T) {
	expected := `
# HELP node_readiness_build_info Build information for the node-readiness-controller binary.
# TYPE node_readiness_build_info gauge
node_readiness_build_info{version="unknown"} 1
`
	if err := testutil.CollectAndCompare(BuildInfo, strings.NewReader(expected), "node_readiness_build_info"); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}

	gathered, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range gathered {
		if mf.GetName() == "node_readiness_build_info" {
			found = true
			if got := mf.GetType().String(); got != "GAUGE" {
				t.Fatalf("expected node_readiness_build_info to be a gauge, got %s", got)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected node_readiness_build_info to be registered with the controller-runtime metrics registry")
	}
}

func TestEvaluationDuration(t *testing.T) {
	t.Run("registered as histogram with expected help text", func(t *testing.T) {
		EvaluationDuration.Reset()
		EvaluationDuration.WithLabelValues("registration-check").Observe(0)
		defer EvaluationDuration.Reset()

		assertMetricRegistered(t, metrics.Registry,
			"node_readiness_evaluation_duration_seconds", "HISTOGRAM",
			"Duration of evaluating a rule against a node, including any taint add/remove operations")
	})

	t.Run("label set is exactly rule", func(t *testing.T) {
		descs := make(chan *prometheus.Desc, 1)
		EvaluationDuration.Describe(descs)
		close(descs)

		desc := <-descs
		if desc == nil {
			t.Fatal("expected EvaluationDuration to yield a descriptor")
		}
		if got, want := desc.String(), "variableLabels: {rule}"; !strings.Contains(got, want) {
			t.Fatalf("expected descriptor to declare exactly one variable label %q, got: %s", want, got)
		}
	})

	t.Run("observation is reflected", func(t *testing.T) {
		EvaluationDuration.Reset()

		EvaluationDuration.WithLabelValues("test-rule").Observe(0.2)

		expected := `
# HELP node_readiness_evaluation_duration_seconds Duration of evaluating a rule against a node, including any taint add/remove operations
# TYPE node_readiness_evaluation_duration_seconds histogram
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="0.005"} 0
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="0.01"} 0
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="0.025"} 0
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="0.05"} 0
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="0.1"} 0
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="0.25"} 1
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="0.5"} 1
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="1"} 1
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="2.5"} 1
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="5"} 1
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="10"} 1
node_readiness_evaluation_duration_seconds_bucket{rule="test-rule",le="+Inf"} 1
node_readiness_evaluation_duration_seconds_sum{rule="test-rule"} 0.2
node_readiness_evaluation_duration_seconds_count{rule="test-rule"} 1
`
		assertObservationReflected(t, EvaluationDuration, "node_readiness_evaluation_duration_seconds", expected)

		if got, want := testutil.CollectAndCount(EvaluationDuration, "node_readiness_evaluation_duration_seconds"), 1; got != want {
			t.Fatalf("expected %d observed series, got %d", want, got)
		}
	})

	t.Run("DeleteLabelValues removes the rule's series", func(t *testing.T) {
		EvaluationDuration.Reset()

		EvaluationDuration.WithLabelValues("delete-me").Observe(0.1)
		if got, want := testutil.CollectAndCount(EvaluationDuration, "node_readiness_evaluation_duration_seconds"), 1; got != want {
			t.Fatalf("expected %d observed series before delete, got %d", want, got)
		}

		if deleted := EvaluationDuration.DeleteLabelValues("delete-me"); !deleted {
			t.Fatal("expected DeleteLabelValues to report that a series was deleted")
		}

		if got, want := testutil.CollectAndCount(EvaluationDuration, "node_readiness_evaluation_duration_seconds"), 0; got != want {
			t.Fatalf("expected %d observed series after delete, got %d", want, got)
		}
	})
}

// assertMetricRegistered checks that the metric is registered correctly.
func assertMetricRegistered(t *testing.T, registry prometheus.Gatherer, name, wantType, wantHelp string) {
	t.Helper()

	gathered, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	for _, mf := range gathered {
		if mf.GetName() != name {
			continue
		}
		if got := mf.GetType().String(); got != wantType {
			t.Fatalf("expected %s to be a %s, got %s", name, wantType, got)
		}
		if got := mf.GetHelp(); got != wantHelp {
			t.Fatalf("unexpected help text for %s: got %q, want %q", name, got, wantHelp)
		}
		return
	}
	t.Fatalf("expected %s to be registered with the controller-runtime metrics registry", name)
}

// assertObservationReflected checks the collected metric.
func assertObservationReflected(t *testing.T, collector prometheus.Collector, name, expectedExposition string) {
	t.Helper()

	if err := testutil.CollectAndCompare(collector, strings.NewReader(expectedExposition), name); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}
}
