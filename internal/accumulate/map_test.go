// SPDX-License-Identifier: MPL-2.0

package accumulate

import (
	"reflect"
	"testing"
)

func TestMergeMap(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		outputs, inputs map[string]string
		want            map[string]string
	}{
		"nil and nil":          {nil, nil, map[string]string{}},
		"nil and empty":        {nil, map[string]string{}, map[string]string{}},
		"new key added":        {map[string]string{"a": "1"}, map[string]string{"b": "2"}, map[string]string{"a": "1", "b": "2"}},
		"existing overwritten": {map[string]string{"a": "1"}, map[string]string{"a": "2"}, map[string]string{"a": "2"}},
		"same value no change": {map[string]string{"a": "1"}, map[string]string{"a": "1"}, map[string]string{"a": "1"}},
		"empty string value":   {map[string]string{"a": "1"}, map[string]string{"a": ""}, map[string]string{"a": ""}},
		"empty inputs":         {map[string]string{"a": "1"}, map[string]string{}, map[string]string{"a": "1"}},
		"empty outputs":        {map[string]string{}, map[string]string{"a": "1"}, map[string]string{"a": "1"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := MergeMap(tc.outputs, tc.inputs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MergeMap(%v, %v) = %v, want %v", tc.outputs, tc.inputs, got, tc.want)
			}
			if got == nil {
				t.Fatal("MergeMap returned nil; it must always return a non-nil map")
			}
		})
	}
}

// TestMergeMapDoesNotMutate pins that callers can keep using the maps they
// passed in: state-derived maps must not change under the resource.
func TestMergeMapDoesNotMutate(t *testing.T) {
	t.Parallel()
	outputs := map[string]string{"a": "1"}
	inputs := map[string]string{"a": "2", "b": "3"}
	got := MergeMap(outputs, inputs)
	got["c"] = "4"
	if !reflect.DeepEqual(outputs, map[string]string{"a": "1"}) {
		t.Fatalf("MergeMap mutated outputs: %v", outputs)
	}
	if !reflect.DeepEqual(inputs, map[string]string{"a": "2", "b": "3"}) {
		t.Fatalf("MergeMap mutated inputs: %v", inputs)
	}
}

func TestNextMapOutputs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		reseed           bool
		planInputs       map[string]string
		stateAccumulated map[string]string
		want             map[string]string
	}{
		"reseed keeps only the planned inputs": {true, map[string]string{"d": "1"}, map[string]string{"a": "1"}, map[string]string{"d": "1"}},
		"merge adds the planned inputs":        {false, map[string]string{"d": "1"}, map[string]string{"a": "1"}, map[string]string{"a": "1", "d": "1"}},
		"merge overwrites a shared key":        {false, map[string]string{"a": "2"}, map[string]string{"a": "1"}, map[string]string{"a": "2"}},
		"merge onto nil history":               {false, map[string]string{"d": "1"}, nil, map[string]string{"d": "1"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := NextMapOutputs(tc.reseed, tc.planInputs, tc.stateAccumulated)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("NextMapOutputs(%v, %v, %v) = %v, want %v", tc.reseed, tc.planInputs, tc.stateAccumulated, got, tc.want)
			}
			if got == nil {
				t.Fatal("NextMapOutputs returned nil; it must always return a non-nil map")
			}
		})
	}
}

func TestSortedKeys(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		m    map[string]string
		want []string
	}{
		"nil":     {nil, []string{}},
		"empty":   {map[string]string{}, []string{}},
		"ordered": {map[string]string{"b": "1", "a": "1", "c": "1"}, []string{"a", "b", "c"}},
		"one":     {map[string]string{"z": ""}, []string{"z"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := SortedKeys(tc.m)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SortedKeys(%v) = %v, want %v", tc.m, got, tc.want)
			}
			if got == nil {
				t.Fatal("SortedKeys returned nil; it must always return a non-nil slice")
			}
		})
	}
}

func TestProject(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		m    map[string]string
		keys []string
		want map[string]string
	}{
		"nil map":          {nil, []string{"a"}, map[string]string{}},
		"nil keys":         {map[string]string{"a": "1"}, nil, map[string]string{}},
		"subset":           {map[string]string{"a": "1", "b": "2"}, []string{"b"}, map[string]string{"b": "2"}},
		"all":              {map[string]string{"a": "1", "b": "2"}, []string{"a", "b"}, map[string]string{"a": "1", "b": "2"}},
		"missing skipped":  {map[string]string{"a": "1"}, []string{"a", "zz"}, map[string]string{"a": "1"}},
		"empty value kept": {map[string]string{"a": ""}, []string{"a"}, map[string]string{"a": ""}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Project(tc.m, tc.keys)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Project(%v, %v) = %v, want %v", tc.m, tc.keys, got, tc.want)
			}
			if got == nil {
				t.Fatal("Project returned nil; it must always return a non-nil map")
			}
		})
	}
}

// TestProjectDoesNotAlias pins that the projection is a new map.
func TestProjectDoesNotAlias(t *testing.T) {
	t.Parallel()
	m := map[string]string{"a": "1"}
	got := Project(m, []string{"a"})
	got["a"] = "changed"
	if m["a"] != "1" {
		t.Fatalf("Project aliased its input: %v", m)
	}
}
