// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import "testing"

func TestHashID(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values []string
		want   string
	}{
		"nil normalizes to an empty list": {
			nil,
			"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945", // sha256("[]")
		},
		"empty slice": {
			[]string{},
			"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945", // sha256("[]")
		},
		"one element": {
			[]string{"a"},
			"0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002", // sha256(`["a"]`)
		},
		"two elements": {
			[]string{"a", "b"},
			"0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13", // sha256(`["a","b"]`)
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			if got := HashID(tc.values); got != tc.want {
				t.Fatalf("HashID(%v) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

// TestHashIDOrderMatters pins that the hash is over the exact list, not over a
// sorted or set-like view of it. accumulator_list treats a reordered list as a
// new value, so the identifier that records the creation inputs must too.
func TestHashIDOrderMatters(t *testing.T) {
	t.Parallel()
	if HashID([]string{"a", "b"}) == HashID([]string{"b", "a"}) {
		t.Fatal("HashID is order-insensitive; it must hash the list as written")
	}
}
