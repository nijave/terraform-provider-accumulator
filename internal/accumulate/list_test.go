// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"testing"
)

func TestTrim(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values []string
		length int
		want   []string
	}{
		"nil values, positive length": {nil, 3, []string{}},
		"empty values":                {[]string{}, 3, []string{}},
		"zero length":                 {[]string{"a", "b"}, 0, []string{}},
		"negative length":             {[]string{"a", "b"}, -1, []string{}},
		"length exceeds values":       {[]string{"a", "b"}, 5, []string{"a", "b"}},
		"length equals values":        {[]string{"a", "b"}, 2, []string{"a", "b"}},
		"keeps the last elements":     {[]string{"a", "b", "c"}, 2, []string{"b", "c"}},
		"length one keeps the last":   {[]string{"a", "b", "c"}, 1, []string{"c"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Trim(tc.values, tc.length)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Trim(%v, %d) = %v, want %v", tc.values, tc.length, got, tc.want)
			}
			if got == nil {
				t.Fatal("Trim returned nil; it must always return a non-nil slice")
			}
		})
	}
}

// TestTrimDoesNotAliasInput guards the slices the provider writes back to
// state: a later append must not mutate the caller's slice. It covers both
// copy branches: the last-N slice and the whole-slice copy taken when length
// meets or exceeds the input length.
func TestTrimDoesNotAliasInput(t *testing.T) {
	t.Parallel()

	for label, tc := range map[string]struct {
		values []string
		length int
	}{
		"last N":       {[]string{"a", "b", "c"}, 2},
		"whole copy":   {[]string{"a", "b", "c"}, 5},
		"exact length": {[]string{"a", "b"}, 2},
	} {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			values := append([]string{}, tc.values...)
			got := Trim(values, tc.length)
			got[0] = "changed"
			if values[0] != tc.values[0] {
				t.Fatalf("Trim aliased its input for %s: values[0] = %q, want %q", label, values[0], tc.values[0])
			}
		})
	}
}

func TestAppend(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		outputs, inputs []string
		length          int
		want            []string
	}{
		"create seeds from inputs":        {nil, []string{"a"}, 1, []string{"a"}},
		"append within length":            {[]string{"a"}, []string{"b"}, 2, []string{"a", "b"}},
		"append trims oldest":             {[]string{"a", "b"}, []string{"c"}, 2, []string{"b", "c"}},
		"append to empty history":         {[]string{}, []string{"a"}, 2, []string{"a"}},
		"inputs truncated on create":      {nil, []string{"a", "b"}, 1, []string{"b"}},
		"whole new list is appended":      {[]string{"a"}, []string{"a", "b"}, 5, []string{"a", "a", "b"}},
		"zero length discards everything": {[]string{"a"}, []string{"b"}, 0, []string{}},
		"empty inputs keeps history":      {[]string{"a"}, []string{}, 1, []string{"a"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Append(tc.outputs, tc.inputs, tc.length)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Append(%v, %v, %d) = %v, want %v", tc.outputs, tc.inputs, tc.length, got, tc.want)
			}
			if got == nil {
				t.Fatal("Append returned nil; it must always return a non-nil slice")
			}
		})
	}
}
