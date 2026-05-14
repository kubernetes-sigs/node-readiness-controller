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

package webhook

import (
	"math"
	"strconv"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
)

// selectorsOverlap returns true if any label set could match both selectors.
func selectorsOverlap(selector1, selector2 labels.Selector) bool {
	reqs1, selectable1 := selector1.Requirements()
	reqs2, selectable2 := selector2.Requirements()
	// A non-selectable selector is labels.Nothing(), which matches no labels
	// and therefore cannot overlap with anything.
	if !selectable1 || !selectable2 {
		return false
	}

	requirementsByKey := map[string][]labels.Requirement{}
	for _, req := range reqs1 {
		requirementsByKey[req.Key()] = append(requirementsByKey[req.Key()], req)
	}
	for _, req := range reqs2 {
		requirementsByKey[req.Key()] = append(requirementsByKey[req.Key()], req)
	}

	for _, reqs := range requirementsByKey {
		if !requirementsOverlap(reqs) {
			return false
		}
	}
	return true
}

func requirementsOverlap(reqs []labels.Requirement) bool {
	var (
		mustExist    bool
		mustNotExist bool
		allowed      sets.Set[string]
		forbidden    sets.Set[string]
		hasLower     bool
		lower        int64
		hasUpper     bool
		upper        int64
	)

	for _, req := range reqs {
		switch req.Operator() {
		case selection.In, selection.Equals, selection.DoubleEquals:
			mustExist = true
			values := sets.New(req.ValuesUnsorted()...)
			if allowed == nil {
				allowed = values
			} else {
				allowed = allowed.Intersection(values)
			}
		case selection.NotIn, selection.NotEquals:
			if forbidden == nil {
				forbidden = sets.Set[string]{}
			}
			forbidden.Insert(req.ValuesUnsorted()...)
		case selection.Exists:
			mustExist = true
		case selection.DoesNotExist:
			mustNotExist = true
		case selection.GreaterThan, selection.LessThan:
			mustExist = true
			values := req.ValuesUnsorted()
			if len(values) != 1 {
				return true
			}
			value, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil {
				return true
			}
			switch req.Operator() {
			case selection.GreaterThan:
				if !hasLower || value > lower {
					hasLower = true
					lower = value
				}
			case selection.LessThan:
				if !hasUpper || value < upper {
					hasUpper = true
					upper = value
				}
			}
		default:
			// Unknown operator: assume overlap defensively, matching the
			// webhook wrapper's "if we can't analyze, assume overlap" stance.
			return true
		}
	}

	if mustNotExist {
		return !mustExist
	}
	if allowed != nil {
		for value := range allowed {
			if valueSatisfies(value, forbidden, hasLower, lower, hasUpper, upper) {
				return true
			}
		}
		return false
	}
	if hasLower || hasUpper {
		return numericValueExists(forbidden, hasLower, lower, hasUpper, upper)
	}
	return true
}

func valueSatisfies(value string, forbidden sets.Set[string], hasLower bool, lower int64, hasUpper bool, upper int64) bool {
	if forbidden.Has(value) {
		return false
	}
	if hasLower || hasUpper {
		intValue, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false
		}
		if hasLower && intValue <= lower {
			return false
		}
		if hasUpper && intValue >= upper {
			return false
		}
	}
	return true
}

// k8s label values must match the label-value regex, which disallows a leading
// '-', so numeric label values are always non-negative; the search space is
// [0, math.MaxInt64].
func numericValueExists(forbidden sets.Set[string], hasLower bool, lower int64, hasUpper bool, upper int64) bool {
	if hasLower && lower == math.MaxInt64 {
		return false
	}
	if hasUpper && upper <= 0 {
		return false
	}

	candidate := int64(0)
	if hasLower {
		candidate = lower + 1
	}
	if candidate < 0 {
		candidate = 0
	}

	for {
		if hasUpper && candidate >= upper {
			return false
		}
		if !forbidden.Has(strconv.FormatInt(candidate, 10)) {
			return true
		}
		if candidate == math.MaxInt64 {
			return false
		}
		candidate++
	}
}
