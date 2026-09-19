// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"testing"
)

// TestNextSetOutputs pins the branch decision the set resource makes on every
// apply. Update and ModifyPlan both call it, so its table is the single place
// the two can drift.
func TestNextSetOutputs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		reseed       bool
		planInputs   []string
		stateOutputs []string
		want         []string
	}{
		"reseed keeps only the planned inputs": {true, []string{"d"}, []string{"a", "b"}, []string{"d"}},
		"union appends the planned inputs":     {false, []string{"d"}, []string{"a", "b"}, []string{"a", "b", "d"}},
		"union is idempotent":                  {false, []string{"a"}, []string{"a", "b"}, []string{"a", "b"}},
		"union onto empty history":             {false, []string{"d"}, nil, []string{"d"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := NextSetOutputs(tc.reseed, tc.planInputs, tc.stateOutputs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("NextSetOutputs(%v, %v, %v) = %v, want %v", tc.reseed, tc.planInputs, tc.stateOutputs, got, tc.want)
			}
			if got == nil {
				t.Fatal("NextSetOutputs returned nil; it must always return a non-nil slice")
			}
		})
	}
}

func TestMerge(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		outputs, inputs []string
		want            []string
	}{
		"nil and nil":               {nil, nil, []string{}},
		"nil and empty":             {nil, []string{}, []string{}},
		"first union":               {[]string{"a"}, []string{"b"}, []string{"a", "b"}},
		"idempotent on overlap":     {[]string{"a", "b"}, []string{"b", "a"}, []string{"a", "b"}},
		"duplicates within inputs":  {[]string{}, []string{"a", "a", "b"}, []string{"a", "b"}},
		"order is first occurrence": {[]string{"c", "a"}, []string{"b", "a"}, []string{"c", "a", "b"}},
		"empty inputs":              {[]string{"a"}, []string{}, []string{"a"}},
		"empty outputs":             {[]string{}, []string{"a"}, []string{"a"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Merge(tc.outputs, tc.inputs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Merge(%v, %v) = %v, want %v", tc.outputs, tc.inputs, got, tc.want)
			}
			if got == nil {
				t.Fatal("Merge returned nil; it must always return a non-nil slice")
			}
		})
	}
}
