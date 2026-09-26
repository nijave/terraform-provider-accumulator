// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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

// listContainsUnknown reports whether the list itself or any of its elements is
// unknown. Terraform marks unknown collection elements individually, so a list
// holding one unknown element is not itself unknown: a plain IsUnknown check
// misses it, and converting such a list to []string would raise a diagnostic.
// ModifyPlan uses this to leave outputs unknown whenever inputs are not fully
// known; Create and Update never run with unknown values.
func listContainsUnknown(list types.List) bool {
	if list.IsUnknown() {
		return true
	}
	for _, element := range list.Elements() {
		if element.IsUnknown() {
			return true
		}
	}
	return false
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

// detailedObjectType is the element type of detailed_outputs on both
// resources: an object whose single attribute is a nullable RFC 3339 string.
var detailedObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{"expires_at": types.StringType},
}

// detailAttr is the Go shape of one detailed_outputs entry. A null
// ExpiresAt means the value cannot expire; it is written as null, never as
// a zero time.
type detailAttr struct {
	ExpiresAt types.String `tfsdk:"expires_at"`
}

// maxExpiresAfter is the largest expires_after (seconds) that can be added
// to a time.Time without the time.Duration conversion overflowing: about
// 292 years. Both resources validate the attribute against it.
const maxExpiresAfter = int64(math.MaxInt64 / int64(time.Second))

// clockNow is the one clock choke point for both the plan-time and
// fallback apply-time reads: UTC truncated to whole seconds, so a stamp
// formatted RFC 3339 parses back to the same instant. A nanosecond stamp
// would lose its fraction on formatting and compare up to a second early on
// the next plan.
func clockNow() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// int64Ptr returns a pointer to the underlying value, or nil for null or
// unknown. Callers on the ModifyPlan path must have resolved unknowns
// already; nil is the "no expires_after" the pure functions expect.
func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	x := v.ValueInt64()
	return &x
}

// stampsFromMap reads detailed_outputs into the value-to-stamp map the pure
// functions take. A null or unknown map (state written before the attribute
// existed) and entries with a null expires_at both mean "no stamp".
func stampsFromMap(ctx context.Context, m types.Map) (map[string]time.Time, diag.Diagnostics) {
	var diags diag.Diagnostics
	stamps := make(map[string]time.Time)
	if m.IsNull() || m.IsUnknown() {
		return stamps, diags
	}
	for key, elem := range m.Elements() {
		obj, ok := elem.(types.Object)
		if !ok {
			diags.AddError("Invalid detailed_outputs state",
				fmt.Sprintf("entry %q is not an object; the state file is corrupt", key))
			return nil, diags
		}
		s, ok := obj.Attributes()["expires_at"].(types.String)
		if !ok {
			diags.AddError("Invalid detailed_outputs state",
				fmt.Sprintf("entry %q has a non-string expires_at; the state file is corrupt", key))
			return nil, diags
		}
		if s.IsNull() || s.IsUnknown() {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, s.ValueString())
		if err != nil {
			diags.AddError("Invalid expires_at in state",
				fmt.Sprintf("entry %q expires_at %q is not an RFC 3339 timestamp: %v", key, s.ValueString(), err))
			return nil, diags
		}
		stamps[key] = stamp
	}
	return stamps, diags
}

// mapFromStamps builds detailed_outputs from the survivors and their
// stamps: one entry per distinct value, expires_at formatted RFC 3339 UTC
// or null when the value has no stamp. Duplicate values in outputs collapse
// into one entry, which is the documented list-resource shape. An empty
// outputs list yields an empty map, never null (the same concreteness rule
// as outputs).
func mapFromStamps(ctx context.Context, outputs []string, stamps map[string]time.Time) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(outputs))
	for _, v := range outputs {
		entry := detailAttr{ExpiresAt: types.StringNull()}
		if stamp, ok := stamps[v]; ok {
			entry.ExpiresAt = types.StringValue(stamp.UTC().Format(time.RFC3339))
		}
		entries[v] = entry
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}

// classifiedMap builds detailed_outputs for the plan: a null expires_at
// for values with no stamp, the formatted prior stamp for keepers (S2),
// and an unknown expires_at for fresh values the apply will stamp (S1,
// S3, S4, S5). The map keys stay known; only the fresh entries' attribute
// is unknown. It reads no clock.
func classifiedMap(ctx context.Context, survivors []string, keep map[string]time.Time, fresh map[string]struct{}) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(survivors))
	for _, v := range survivors {
		entry := detailAttr{ExpiresAt: types.StringNull()}
		if _, ok := fresh[v]; ok {
			entry.ExpiresAt = types.StringUnknown()
		} else if stamp, ok := keep[v]; ok {
			entry.ExpiresAt = types.StringValue(stamp.UTC().Format(time.RFC3339))
		}
		entries[v] = entry
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}
