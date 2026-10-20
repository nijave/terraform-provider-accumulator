// SPDX-License-Identifier: MPL-2.0

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

func TestHashMapID(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values map[string]string
		want   string
	}{
		"nil normalizes to an empty object": {
			nil,
			"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", // sha256("{}")
		},
		"empty map": {
			map[string]string{},
			"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", // sha256("{}")
		},
		"one entry": {
			map[string]string{"a": "1"},
			"9afeb0f2b203f254312ec8ded441d0318b7c34c57f8695ede42d2215a30c0960", // sha256(`{"a":"1"}`)
		},
		"two entries": {
			map[string]string{"b": "1", "a": "1"},
			"40239544dc03c3ec3a09cb947c72123697428a72af96b1c63431f34a46517709", // sha256(`{"a":"1","b":"1"}`)
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			if got := HashMapID(tc.values); got != tc.want {
				t.Fatalf("HashMapID(%v) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

// TestHashMapIDDiffersFromKeyList pins that a map hashes its JSON object,
// not its key list: {"a":"1"} and ["a"] must not collide.
func TestHashMapIDDiffersFromKeyList(t *testing.T) {
	t.Parallel()
	if HashMapID(map[string]string{"a": "1"}) == HashID([]string{"a"}) {
		t.Fatal("HashMapID collides with HashID of the key list")
	}
}
