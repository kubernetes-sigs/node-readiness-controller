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

package controller

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetBootstrapState(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name        string
		annotations map[string]string
		expected    map[string]bool
	}{
		{
			name:        "nil annotations returns empty map",
			annotations: nil,
			expected:    map[string]bool{},
		},
		{
			name:        "missing annotation key returns empty map",
			annotations: map[string]string{"other-key": "other-value"},
			expected:    map[string]bool{},
		},
		{
			name:        "empty annotation value returns empty map",
			annotations: map[string]string{bootstrapStateAnnotation: ""},
			expected:    map[string]bool{},
		},
		{
			name:        "malformed JSON returns empty map",
			annotations: map[string]string{bootstrapStateAnnotation: "not-json"},
			expected:    map[string]bool{},
		},
		{
			name:        "valid JSON with one rule",
			annotations: map[string]string{bootstrapStateAnnotation: `{"my-rule":true}`},
			expected:    map[string]bool{"my-rule": true},
		},
		{
			name:        "valid JSON with multiple rules",
			annotations: map[string]string{bootstrapStateAnnotation: `{"rule-a":true,"rule-b":true}`},
			expected:    map[string]bool{"rule-a": true, "rule-b": true},
		},
		{
			name: "very long rule name stored as map key — no length constraint",
			annotations: map[string]string{
				bootstrapStateAnnotation: `{"my-very-long-rule-name-that-is-definitely-going-to-exceed-the-63-character-annotation-key-limit":true}`,
			},
			expected: map[string]bool{
				"my-very-long-rule-name-that-is-definitely-going-to-exceed-the-63-character-annotation-key-limit": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			result := getBootstrapState(node)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

func TestSerializeBootstrapState(t *testing.T) {
	g := NewWithT(t)

	t.Run("empty map serializes to {}", func(t *testing.T) {
		result := serializeBootstrapState(map[string]bool{})
		g.Expect(result).To(Equal("{}"))
	})

	t.Run("round-trip: serialize then deserialize preserves entries", func(t *testing.T) {
		original := map[string]bool{
			"rule-a": true,
			"my-very-long-rule-name-that-exceeds-any-annotation-key-limit-by-far": true,
		}
		serialized := serializeBootstrapState(original)

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{bootstrapStateAnnotation: serialized},
			},
		}
		recovered := getBootstrapState(node)
		g.Expect(recovered).To(Equal(original))
	})
}

