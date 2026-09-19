// SPDX-License-Identifier: GPL-3.0-or-later

// Package accumulate holds the list and set behavior of the accumulator
// resources as pure Go. It imports no Terraform packages, so every decision it
// makes is unit-testable without a plugin harness and internal/provider stays a
// mechanical translation.
package accumulate

// Trim returns the last length elements of values. A length <= 0 yields an
// empty slice; a length >= len(values) yields the whole slice. A nil or empty
// input yields an empty (non-nil) slice, so the value written back to state is
// always concrete and downstream JSON is "[]" rather than null.
//
// The result never aliases values, so a caller can append to it without
// mutating the slice it read from state.
func Trim(values []string, length int) []string {
	if length <= 0 || len(values) == 0 {
		return []string{}
	}
	if length >= len(values) {
		return append([]string{}, values...)
	}
	return append([]string{}, values[len(values)-length:]...)
}

// Append returns Trim(outputs ++ inputs, length).
//
// There is deliberately no deduplication: re-submitting a value appends it
// again. Only accumulator_set deduplicates.
func Append(outputs, inputs []string, length int) []string {
	combined := make([]string, 0, len(outputs)+len(inputs))
	combined = append(combined, outputs...)
	combined = append(combined, inputs...)
	return Trim(combined, length)
}

// NextListOutputs returns the outputs accumulator_list writes for the next
// apply, whether that apply is planned (ModifyPlan) or performed (Update). It
// is the whole branch decision of the accumulation algorithm:
//
//   - reseed (a replacement or a triggers_reset change) discards history and
//     seeds outputs from the planned inputs alone;
//   - otherwise an inputs change appends the whole planned list to the prior
//     history;
//   - otherwise only length changed and the existing history is re-trimmed.
//
// Callers pass reseed for a replacement exactly as they pass it for a reset:
// Create and Update produce the same outputs for both.
func NextListOutputs(reseed, inputsChanged bool, planInputs, stateOutputs []string, length int) []string {
	switch {
	case reseed:
		return Trim(planInputs, length)
	case inputsChanged:
		return Append(stateOutputs, planInputs, length)
	default:
		return Trim(stateOutputs, length)
	}
}
