// SPDX-License-Identifier: MPL-2.0

package accumulate

import "sort"

// MergeMap returns a copy of outputs with every entry of inputs written
// over it: a new key is added and an existing key takes the input value
// (spec M1, overwrite in place). Neither argument is mutated. The result is
// never nil, so the value written to state is a concrete map.
//
// It is idempotent: applying it whenever the resource is updated is correct
// whether or not the inputs changed, which removes the need for the resource
// to compare planned inputs against state.
func MergeMap(outputs, inputs map[string]string) map[string]string {
	merged := make(map[string]string, len(outputs)+len(inputs))
	for k, v := range outputs {
		merged[k] = v
	}
	for k, v := range inputs {
		merged[k] = v
	}
	return merged
}

// NextMapOutputs returns the accumulated map accumulator_map writes for the
// next apply, whether that apply is planned (ModifyPlan) or performed
// (Update). It is the whole branch decision: reseed (a replacement or a
// triggers_reset change) discards history and seeds from the planned inputs
// alone (spec M2); otherwise the planned inputs are merged over the prior
// map (spec M1).
func NextMapOutputs(reseed bool, planInputs, stateAccumulated map[string]string) map[string]string {
	if reseed {
		return MergeMap(nil, planInputs)
	}
	return MergeMap(stateAccumulated, planInputs)
}

// SortedKeys returns the keys of m in ascending order. A nil or empty m
// yields an empty (non-nil) slice. The stamp functions give the same result
// for any key order, so correctness does not depend on this; the map
// resource passes sorted keys so that their output and any debugging trace
// are stable from run to run.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Project returns the entries of m whose keys are in keys, as a new map. It
// turns a key list returned by a stamp function (survivors, visible) back
// into the map(string) the resource writes. A key absent from m is skipped.
// The result is never nil and never aliases m.
func Project(m map[string]string, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}
