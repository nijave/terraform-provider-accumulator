// SPDX-License-Identifier: MPL-2.0

package accumulate

import (
	"encoding/json"
	"errors"
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
// full payload: a diagnostic reaches the console and CI logs. A duplicated
// top-level key is decoded last-wins by encoding/json and is not rejected.
func ParseImport(id string) (ImportSeed, error) {
	seed := ImportSeed{Inputs: []string{}, Outputs: []string{}}
	err := parseSeed(id, stringArray,
		"import ID must be a JSON object with optional \"inputs\" and \"outputs\" arrays of strings, "+
			"for example {\"inputs\":[\"a\"],\"outputs\":[\"a\",\"b\"]}",
		func(k string, v []string) {
			if k == "inputs" {
				seed.Inputs = v
			} else {
				seed.Outputs = v
			}
		})
	if err != nil {
		return ImportSeed{}, err
	}
	return seed, nil
}

// parseSeed decodes the top-level import object, validates and sorts its keys,
// and decodes each value with decode. shape is the full message for a payload
// that is not an object. set receives each decoded value by key.
func parseSeed[T any](id string, decode func(json.RawMessage) (T, error), shape string, set func(key string, v T)) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(id), &object); err != nil || object == nil {
		return errors.New(shape)
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
			return fmt.Errorf(
				"import ID key %q is not recognized; the only recognized keys are \"inputs\" and \"outputs\"", k)
		}
		values, err := decode(object[k])
		if err != nil {
			return fmt.Errorf("import ID key %q %v", k, err)
		}
		set(k, values)
	}
	return nil
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

// MapImportSeed is the parsed form of an accumulator_map import ID. A
// missing key is an empty map, never nil, so the caller always writes a
// concrete map to state.
type MapImportSeed struct {
	Inputs  map[string]string
	Outputs map[string]string
}

// ParseMapImport parses an accumulator_map import ID, which is a JSON
// object whose values are objects of strings:
//
//	{"inputs":{"k":"v"},"outputs":{"k":"v","k2":"v2"}}
//
// The same contract as ParseImport: only "inputs" and "outputs" are
// recognized, each must be a JSON object whose values are strings, and
// errors name the expected shape and quote the offending key only. A
// duplicated top-level key is decoded last-wins by encoding/json and is not
// rejected.
func ParseMapImport(id string) (MapImportSeed, error) {
	seed := MapImportSeed{Inputs: map[string]string{}, Outputs: map[string]string{}}
	err := parseSeed(id, stringObject,
		"import ID must be a JSON object with optional \"inputs\" and \"outputs\" objects of strings, "+
			"for example {\"inputs\":{\"k\":\"v\"},\"outputs\":{\"k\":\"v\"}}",
		func(k string, v map[string]string) {
			if k == "inputs" {
				seed.Inputs = v
			} else {
				seed.Outputs = v
			}
		})
	if err != nil {
		return MapImportSeed{}, err
	}
	return seed, nil
}

// stringObject decodes a JSON value that must be an object of strings. A
// JSON null decodes into a nil map with no error, so it is rejected
// explicitly, as are arrays, numbers, nested objects, and booleans inside
// the object.
func stringObject(raw json.RawMessage) (map[string]string, error) {
	var items map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, fmt.Errorf("must be an object of strings")
	}
	out := make(map[string]string, len(items))
	for k, item := range items {
		var s string
		if err := json.Unmarshal(item, &s); err != nil {
			return nil, fmt.Errorf("must be an object of strings")
		}
		out[k] = s
	}
	return out, nil
}
