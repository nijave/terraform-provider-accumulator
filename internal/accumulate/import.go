// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ImportSeed is the parsed form of an import ID. A missing key is an empty
// slice, never nil, so the caller always writes a concrete list to state.
type ImportSeed struct {
	Inputs  []string
	Outputs []string
}

// ParseImport parses an import ID, which is a JSON object whose values are
// arrays of strings:
//
//	{"inputs":["a","b"],"outputs":["a","b","c"]}
//
// A resource with no external system to discover it from cannot be imported by
// a plain passthrough ID, so the ID carries the seed itself.
//
// Errors name the expected shape and quote the offending key only, never the
// full payload: a diagnostic reaches the console and CI logs.
func ParseImport(id string) (ImportSeed, error) {
	seed := ImportSeed{Inputs: []string{}, Outputs: []string{}}

	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(id), &object); err != nil || object == nil {
		return ImportSeed{}, fmt.Errorf(
			"import ID must be a JSON object with optional \"inputs\" and \"outputs\" arrays of strings, " +
				"for example {\"inputs\":[\"a\"],\"outputs\":[\"a\",\"b\"]}")
	}

	// Sorted so a payload with more than one mistake reports the same key every
	// run rather than whichever the map happened to yield first.
	keys := make([]string, 0, len(object))
	for k := range object {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if k != "inputs" && k != "outputs" {
			return ImportSeed{}, fmt.Errorf(
				"import ID key %q is not recognized; the only recognized keys are \"inputs\" and \"outputs\"", k)
		}
		values, err := stringArray(object[k])
		if err != nil {
			return ImportSeed{}, fmt.Errorf("import ID key %q %v", k, err)
		}
		if k == "inputs" {
			seed.Inputs = values
		} else {
			seed.Outputs = values
		}
	}
	return seed, nil
}

// stringArray decodes a JSON value that must be an array of strings. A JSON
// null decodes into a nil slice with no error, so it is rejected explicitly,
// as are numbers, objects, and booleans nested inside the array.
func stringArray(raw json.RawMessage) ([]string, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, fmt.Errorf("must be an array of strings")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		var s string
		if err := json.Unmarshal(item, &s); err != nil {
			return nil, fmt.Errorf("must be an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}
