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
