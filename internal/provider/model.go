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
// resources: an object with two nullable RFC 3339 strings. expires_at is
// when the value leaves accumulated (null: cannot expire); visible_at is
// when a pending value joins outputs (null: already visible).
var detailedObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"expires_at": types.StringType,
		"visible_at": types.StringType,
	},
}

// detailAttr is the Go shape of one detailed_outputs entry. A null stamp
// is written as null, never as a zero time.
type detailAttr struct {
	ExpiresAt types.String `tfsdk:"expires_at"`
	VisibleAt types.String `tfsdk:"visible_at"`
}

// stamps holds both stamp kinds for a set of values, keyed by value.
// Absence from a map means the corresponding attribute is null.
type stamps struct {
	Expires map[string]time.Time
	Visible map[string]time.Time
}

// maxExpiresAfter is the largest expires_after (seconds) that can be added
// to a time.Time without the time.Duration conversion overflowing: about
// 292 years. Both resources check `expires_after` and `delayed_for` against it.
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

// stampsFromMap reads detailed_outputs into the two value-to-stamp maps
// the pure functions take. A null or unknown map (state written before
// the attribute existed) yields empty maps; an entry whose attribute is
// null, unknown, or absent (state written before visible_at existed,
// spec section 9) contributes no stamp. Both maps are non-nil.
func stampsFromMap(ctx context.Context, m types.Map) (stamps, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := stamps{
		Expires: make(map[string]time.Time),
		Visible: make(map[string]time.Time),
	}
	if m.IsNull() || m.IsUnknown() {
		return out, diags
	}
	for key, elem := range m.Elements() {
		obj, ok := elem.(types.Object)
		if !ok {
			diags.AddError("Invalid detailed_outputs state",
				fmt.Sprintf("entry %q is not an object; the state file is corrupt", key))
			return out, diags
		}
		attrs := obj.Attributes()
		for name, target := range map[string]map[string]time.Time{
			"expires_at": out.Expires,
			"visible_at": out.Visible,
		} {
			raw, present := attrs[name]
			if !present {
				continue
			}
			s, ok := raw.(types.String)
			if !ok {
				diags.AddError("Invalid detailed_outputs state",
					fmt.Sprintf("entry %q has a non-string %s; the state file is corrupt", key, name))
				return out, diags
			}
			if s.IsNull() || s.IsUnknown() {
				continue
			}
			stamp, err := time.Parse(time.RFC3339, s.ValueString())
			if err != nil {
				diags.AddError(fmt.Sprintf("Invalid %s in state", name),
					fmt.Sprintf("entry %q %s %q is not an RFC 3339 timestamp: %v", key, name, s.ValueString(), err))
				return out, diags
			}
			target[key] = stamp
		}
	}
	return out, diags
}

// formatStamp renders a stamp RFC 3339 UTC, or null when absent.
func formatStamp(m map[string]time.Time, v string) types.String {
	if stamp, ok := m[v]; ok {
		return types.StringValue(stamp.UTC().Format(time.RFC3339))
	}
	return types.StringNull()
}

// mapFromStamps builds detailed_outputs from values and their stamps: one
// entry per distinct value, each attribute formatted RFC 3339 UTC or null.
// Duplicate values collapse into one entry, which is the documented
// list-resource shape. An empty values yields an empty map, never null.
func mapFromStamps(ctx context.Context, values []string, s stamps) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(values))
	for _, v := range values {
		entries[v] = detailAttr{
			ExpiresAt: formatStamp(s.Expires, v),
			VisibleAt: formatStamp(s.Visible, v),
		}
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}

// classified is the clock-free plan-time decision for every value in
// accumulated: which stamps survive as known values and which the apply
// must create. A value absent from all four is null on both attributes.
type classified struct {
	KeepExpires  map[string]time.Time
	KeepVisible  map[string]time.Time
	FreshExpires map[string]struct{}
	FreshVisible map[string]struct{}
}

// classifiedMap builds detailed_outputs for the plan: a null attribute for
// values with no stamp, the formatted prior stamp for keepers, and an
// unknown attribute for fresh entries the apply will stamp. The map keys
// stay known; only fresh attributes are unknown. It reads no clock.
func classifiedMap(ctx context.Context, values []string, c classified) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(values))
	for _, v := range values {
		entry := detailAttr{
			ExpiresAt: formatStamp(c.KeepExpires, v),
			VisibleAt: formatStamp(c.KeepVisible, v),
		}
		if _, ok := c.FreshExpires[v]; ok {
			entry.ExpiresAt = types.StringUnknown()
		}
		if _, ok := c.FreshVisible[v]; ok {
			entry.VisibleAt = types.StringUnknown()
		}
		entries[v] = entry
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}
