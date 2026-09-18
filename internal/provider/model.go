// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// stringsFromList converts a list(string) to a Go slice. Null and unknown are
// both treated as absent and produce a nil slice with no diagnostic: a null
// required attribute is a schema violation the framework already reports, and
// an unknown value is resolved before Create or Update runs.
func stringsFromList(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return nil, diags
	}
	var out []string
	diags.Append(list.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// stringsFromSet is stringsFromList for a set(string). Element order is not
// meaningful for a set; the result is only ever fed to accumulate.Merge, which
// is a union.
func stringsFromSet(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if set.IsNull() || set.IsUnknown() {
		return nil, diags
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// listFromStrings converts a Go slice to a concrete list(string). A nil slice
// becomes an empty list, never null: an empty accumulated history must be
// written as [] so state round-trips without a diff against an empty config.
func listFromStrings(ctx context.Context, values []string) (types.List, diag.Diagnostics) {
	if values == nil {
		values = []string{}
	}
	return types.ListValueFrom(ctx, types.StringType, values)
}

// setFromStrings is listFromStrings for a set(string). values must already be
// unique: the framework reports a duplicate as a diagnostic rather than
// collapsing it silently. Every caller passes accumulate.Merge's output, which
// is unique, so a diagnostic here means a caller bug rather than bad input.
func setFromStrings(ctx context.Context, values []string) (types.Set, diag.Diagnostics) {
	if values == nil {
		values = []string{}
	}
	return types.SetValueFrom(ctx, types.StringType, values)
}
