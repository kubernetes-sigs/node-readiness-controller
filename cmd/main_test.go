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

package main

import (
	"strings"
	"testing"
)

func TestValidateKubeAPIFlags(t *testing.T) {
	tests := []struct {
		name    string
		qps     float64
		burst   int
		wantErr bool
	}{
		{"both at the disabled default", defaultKubeAPIQPS, defaultKubeAPIBurst, false},
		{"qps set, burst set", 50, 100, false},
		{"qps set, burst at the default", 50, defaultKubeAPIBurst, true},
		{"qps set, burst zero", 50, 0, true},
		{"burst set, qps at the default", defaultKubeAPIQPS, 100, false},
		{"qps zero, burst at the default", 0, defaultKubeAPIBurst, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKubeAPIFlags(tt.qps, tt.burst)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateKubeAPIFlags(%v, %d) error = %v, wantErr %v", tt.qps, tt.burst, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "--kube-api-burst") {
				t.Fatalf("error should name the missing flag, got %q", err.Error())
			}
		})
	}
}
