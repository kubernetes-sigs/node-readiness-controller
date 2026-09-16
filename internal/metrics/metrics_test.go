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
