// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseImport(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		id   string
		want ImportSeed
	}{
		"list shape": {
			`{"inputs":["a","b"],"outputs":["a","b","c"]}`,
			ImportSeed{Inputs: []string{"a", "b"}, Outputs: []string{"a", "b", "c"}},
		},
		"set shape": {
			`{"outputs":["a","b"]}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{"a", "b"}},
		},
		"empty object": {
			`{}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{}},
		},
		"missing inputs": {
			`{"outputs":["a"]}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{"a"}},
		},
		"missing outputs": {
			`{"inputs":["a"]}`,
			ImportSeed{Inputs: []string{"a"}, Outputs: []string{}},
		},
		"empty arrays stay empty": {
			`{"inputs":[],"outputs":[]}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{}},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, err := ParseImport(tc.id)
			if err != nil {
				t.Fatalf("ParseImport(%q): %v", tc.id, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseImport(%q) = %+v, want %+v", tc.id, got, tc.want)
			}
			if got.Inputs == nil || got.Outputs == nil {
				t.Fatal("ParseImport returned a nil slice; a missing key must mean empty, not null")
			}
		})
	}
}

func TestParseImportRejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		id     string
		substr string
	}{
		"invalid json":         {`not json`, "must be a JSON object"},
		"bare array":           {`["a"]`, "must be a JSON object"},
		"bare scalar":          {`"a"`, "must be a JSON object"},
		"json null":            {`null`, "must be a JSON object"},
		"empty id":             {``, "must be a JSON object"},
		"unknown key":          {`{"outupts":["a"]}`, "not recognized"},
		"unknown key is named": {`{"outupts":["a"]}`, `"outupts"`},
		"inputs not an array":  {`{"inputs":"a"}`, "must be an array of strings"},
		"inputs null":          {`{"inputs":null}`, "must be an array of strings"},
		"inputs numbers":       {`{"inputs":[1,2]}`, "must be an array of strings"},
		"inputs nested object": {`{"inputs":[{"a":1}]}`, "must be an array of strings"},
		"outputs not an array": {`{"outputs":1}`, "must be an array of strings"},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			_, err := ParseImport(tc.id)
			if err == nil {
				t.Fatalf("ParseImport(%q) returned nil error, want one", tc.id)
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("ParseImport(%q) error = %q, want it to contain %q", tc.id, err.Error(), tc.substr)
			}
		})
	}
}

// TestParseImportErrorDoesNotEchoPayload guards the diagnostic surface: the
// error must name the offending key, never repeat the whole import ID.
func TestParseImportErrorDoesNotEchoPayload(t *testing.T) {
	t.Parallel()
	_, err := ParseImport(`{"inputs":["secret-a","secret-b"],"outupts":["secret-c"]}`)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{"secret-a", "secret-b", "secret-c"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q echoes %q from the payload", err.Error(), secret)
		}
	}
}
