// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

// Merge returns the union of outputs and inputs, first occurrence wins, with a
// stable order so the value written to state is deterministic. A nil or empty
// input yields an empty (non-nil) slice.
//
// It is idempotent: applying it whenever the resource is updated is correct
// whether or not the inputs changed, which removes the need for the resource to
// compare planned inputs against state.
func Merge(outputs, inputs []string) []string {
	merged := make([]string, 0, len(outputs)+len(inputs))
	seen := make(map[string]struct{}, len(outputs)+len(inputs))

	for _, group := range [2][]string{outputs, inputs} {
		for _, v := range group {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			merged = append(merged, v)
		}
	}
	return merged
}

// NextSetOutputs returns the outputs accumulator_set writes for the next
// apply, whether that apply is planned (ModifyPlan) or performed (Update). It
// is the whole branch decision of the accumulation algorithm: reseed (a
// replacement or a triggers_reset change) discards history and seeds outputs
// from the planned inputs alone; otherwise the planned inputs are unioned into
// the prior set.
//
// Callers pass reseed for a replacement exactly as they pass it for a reset:
// Create and Update produce the same outputs for both.
func NextSetOutputs(reseed bool, planInputs, stateOutputs []string) []string {
	if reseed {
		return Merge(nil, planInputs)
	}
	return Merge(stateOutputs, planInputs)
}
