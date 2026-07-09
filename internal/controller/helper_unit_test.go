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
	"k8s.io/apimachinery/pkg/types"
)

func TestBootstrapAnnotationKey(t *testing.T) {
	g := NewWithT(t)

	uid := types.UID("550e8400-e29b-41d4-a716-446655440000")
	key := bootstrapAnnotationKey(uid)
	g.Expect(key).To(Equal("readiness.k8s.io/bootstrap-completed-550e8400-e29b-41d4-a716-446655440000"))
}

func TestBootstrapAnnotationValue(t *testing.T) {
	g := NewWithT(t)

	t.Run("encodes rule name as JSON", func(t *testing.T) {
		val := bootstrapAnnotationValue("my-rule")
		g.Expect(val).To(Equal(`{"rule-name":"my-rule"}`))
	})

	t.Run("handles long rule names", func(t *testing.T) {
		longName := "my-very-long-rule-name-that-exceeds-the-63-character-annotation-key-limit-strictly"
		val := bootstrapAnnotationValue(longName)
		g.Expect(val).To(ContainSubstring(longName))
	})
}

func TestLegacyBootstrapAnnotationKey(t *testing.T) {
	g := NewWithT(t)

	key := legacyBootstrapAnnotationKey("my-rule")
	g.Expect(key).To(Equal("readiness.k8s.io/bootstrap-completed-my-rule"))
}
