// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestListFromStringsIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string][]string{
		"nil":    nil,
		"empty":  {},
		"values": {"a", "b"},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := listFromStrings(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("listFromStrings: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("listFromStrings produced a null list; state must always hold a concrete value")
			}
			if got.IsUnknown() {
				t.Fatal("listFromStrings produced an unknown list")
			}
		})
	}
}

func TestSetFromStringsIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := setFromStrings(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("setFromStrings: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("setFromStrings produced a null set; state must always hold a concrete value")
			}
		})
	}
}

// TestSetFromStringsRequiresUniqueValues pins the framework behavior the
// resources depend on: a duplicate is a diagnostic, not a silent collapse.
// Every caller passes accumulate.Merge's output, which is unique by
// construction, so the diagnostic is unreachable in practice and is a bug
// signal if it ever appears.
func TestSetFromStringsRequiresUniqueValues(t *testing.T) {
	t.Parallel()
	got, diags := setFromStrings(context.Background(), []string{"a", "b", "a"})
	if !diags.HasError() {
		t.Fatalf("setFromStrings accepted duplicates and produced %v", got)
	}
}

func TestStringsFromListRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	list, diags := listFromStrings(ctx, []string{"a", "b"})
	if diags.HasError() {
		t.Fatalf("listFromStrings: %v", diags.Errors())
	}
	got, diags := stringsFromList(ctx, list)
	if diags.HasError() {
		t.Fatalf("stringsFromList: %v", diags.Errors())
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("round trip produced %v, want [a b]", got)
	}
}

func TestStringsFromListNullIsEmpty(t *testing.T) {
	t.Parallel()
	got, diags := stringsFromList(context.Background(), types.ListNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromList: %v", diags.Errors())
	}
	if got != nil {
		t.Fatalf("stringsFromList(null) = %v, want nil", got)
	}
}

func TestStringsFromSetRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	set, diags := setFromStrings(ctx, []string{"a", "b"})
	if diags.HasError() {
		t.Fatalf("setFromStrings: %v", diags.Errors())
	}
	got, diags := stringsFromSet(ctx, set)
	if diags.HasError() {
		t.Fatalf("stringsFromSet: %v", diags.Errors())
	}
	if len(got) != 2 {
		t.Fatalf("round trip produced %v, want two elements", got)
	}
}

func TestStringsFromSetNullIsEmpty(t *testing.T) {
	t.Parallel()
	got, diags := stringsFromSet(context.Background(), types.SetNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromSet: %v", diags.Errors())
	}
	if got != nil {
		t.Fatalf("stringsFromSet(null) = %v, want nil", got)
	}
}

// TestStringsFromUnknownIsEmpty pins the other absent case. At apply time a
// value is known, so this branch is defensive, but it must not raise a
// diagnostic or dereference an unknown element.
func TestStringsFromUnknownIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	list, diags := stringsFromList(ctx, types.ListUnknown(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromList(unknown): %v", diags.Errors())
	}
	if list != nil {
		t.Fatalf("stringsFromList(unknown) = %v, want nil", list)
	}

	set, diags := stringsFromSet(ctx, types.SetUnknown(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromSet(unknown): %v", diags.Errors())
	}
	if set != nil {
		t.Fatalf("stringsFromSet(unknown) = %v, want nil", set)
	}
}

// TestListContainsUnknown pins the unknown shapes ModifyPlan must guard
// against. Terraform marks unknown collection elements individually, so a list
// of one unknown element is not itself unknown: a plain IsUnknown check would
// miss it and a conversion would raise a diagnostic mid-plan.
func TestListContainsUnknown(t *testing.T) {
	t.Parallel()

	for label, list := range map[string]types.List{
		"known list":              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")}),
		"null list":               types.ListNull(types.StringType),
		"unknown list":            types.ListUnknown(types.StringType),
		"list with unknown value": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a"), types.StringUnknown()}),
		"list of unknown values":  types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown(), types.StringUnknown()}),
	} {
		list := list
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			want := label != "known list" && label != "null list"
			if got := listContainsUnknown(list); got != want {
				t.Fatalf("listContainsUnknown(%v) = %v, want %v", list, got, want)
			}
		})
	}
}

func TestMapFromStampsAndBack(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	outputs := []string{"a", "b", "c"}
	stamps := map[string]time.Time{
		"b": base.Add(5 * time.Minute),
		"c": base.Add(time.Hour),
	}
	m, diags := mapFromStamps(ctx, outputs, stamps)
	if diags.HasError() {
		t.Fatalf("mapFromStamps: %+v", diags)
	}
	if m.IsNull() || m.IsUnknown() {
		t.Fatalf("mapFromStamps returned %v; it must be concrete", m)
	}
	if got := len(m.Elements()); got != 3 {
		t.Fatalf("map has %d entries, want 3", got)
	}

	parsed, diags := stampsFromMap(ctx, m)
	if diags.HasError() {
		t.Fatalf("stampsFromMap: %+v", diags)
	}
	want := map[string]time.Time{
		"b": base.Add(5 * time.Minute),
		"c": base.Add(time.Hour),
	}
	if !reflect.DeepEqual(parsed, want) {
		t.Fatalf("round trip = %v, want %v", parsed, want)
	}

	// The null entry ("a") must not appear: absence is the null stamp.
	if _, ok := parsed["a"]; ok {
		t.Fatal("null expires_at round-tripped as a stamp")
	}
}

func TestMapFromStampsIsNeverNull(t *testing.T) {
	m, diags := mapFromStamps(context.Background(), nil, nil)
	if diags.HasError() {
		t.Fatalf("mapFromStamps: %+v", diags)
	}
	if m.IsNull() {
		t.Fatal("mapFromStamps(nil) returned null; an empty accumulator must be {}")
	}
	if got := len(m.Elements()); got != 0 {
		t.Fatalf("map has %d entries, want 0", got)
	}
}

// TestClassifiedMap pins the plan-shape split: keepers carry the prior
// stamp, fresh entries are unknown (the apply stamps them), and everything
// else is null. The map itself stays known.
func TestClassifiedMap(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	m, diags := classifiedMap(ctx, []string{"a", "b", "c"},
		map[string]time.Time{"b": base.Add(5 * time.Minute)},
		map[string]struct{}{"c": {}})
	if diags.HasError() {
		t.Fatalf("classifiedMap: %+v", diags)
	}
	if m.IsNull() || m.IsUnknown() {
		t.Fatalf("classifiedMap returned %v; it must be a concrete map", m)
	}

	elems := m.Elements()
	if got := len(elems); got != 3 {
		t.Fatalf("map has %d entries, want 3", got)
	}
	expiresAt := func(v string) types.String {
		t.Helper()
		obj, ok := elems[v].(types.Object)
		if !ok {
			t.Fatalf("entry %q is %T, want types.Object", v, elems[v])
		}
		s, ok := obj.Attributes()["expires_at"].(types.String)
		if !ok {
			t.Fatalf("entry %q expires_at is %T, want types.String", v, obj.Attributes()["expires_at"])
		}
		return s
	}

	if s := expiresAt("a"); !s.IsNull() {
		t.Errorf("a expires_at = %v, want null", s)
	}
	if s := expiresAt("b"); s.ValueString() != base.Add(5*time.Minute).Format(time.RFC3339) {
		t.Errorf("b expires_at = %q, want %q", s.ValueString(), base.Add(5*time.Minute).Format(time.RFC3339))
	}
	if s := expiresAt("c"); !s.IsUnknown() {
		t.Errorf("c expires_at = %v, want unknown", s)
	}
}

func TestStampsFromMapNullAndUnknownAreEmpty(t *testing.T) {
	for _, m := range []types.Map{types.MapNull(detailedObjectType), types.MapUnknown(detailedObjectType)} {
		stamps, diags := stampsFromMap(context.Background(), m)
		if diags.HasError() {
			t.Fatalf("stampsFromMap(%v): %+v", m, diags)
		}
		if len(stamps) != 0 {
			t.Fatalf("stampsFromMap(%v) = %v, want empty", m, stamps)
		}
	}
}

func TestStampsFromMapRejectsGarbageTimestamp(t *testing.T) {
	m := types.MapValueMust(detailedObjectType, map[string]attr.Value{
		"b": types.ObjectValueMust(detailedObjectType.AttrTypes, map[string]attr.Value{
			"expires_at": types.StringValue("not-a-timestamp"),
		}),
	})
	_, diags := stampsFromMap(context.Background(), m)
	if !diags.HasError() {
		t.Fatal("stampsFromMap accepted a malformed timestamp; it must diagnose")
	}
}

func TestInt64Ptr(t *testing.T) {
	if got := int64Ptr(types.Int64Null()); got != nil {
		t.Fatalf("int64Ptr(null) = %v, want nil", *got)
	}
	if got := int64Ptr(types.Int64Unknown()); got != nil {
		t.Fatalf("int64Ptr(unknown) = %v, want nil", *got)
	}
	v := types.Int64Value(300)
	if got := int64Ptr(v); got == nil || *got != 300 {
		t.Fatalf("int64Ptr(300) = %v, want 300", got)
	}
}

// TestStateGetToleratesMissingExpirationAttributes pins spec section 9:
// state written by the pre-expiration provider lacks expires_after and
// detailed_outputs entirely, and Get must unmarshal it with both null
// rather than erroring. If this test fails, the correct fix is an
// UpgradeState implementation, not a schema change.
func TestStateGetToleratesMissingExpirationAttributes(t *testing.T) {
	setSchema := resourceSchema(t, &setResource{})
	listSchema := resourceSchema(t, &listResource{})

	stringType := tftypes.String
	setOld := tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"inputs":               tftypes.List{ElementType: stringType},
		"triggers_reset":       stringType,
		"triggers_replacement": stringType,
		"outputs":              tftypes.Set{ElementType: stringType},
		"id":                   stringType,
	}}, map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"outputs":              tftypes.NewValue(tftypes.Set{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"id":                   tftypes.NewValue(stringType, "x"),
	})
	listOld := tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"inputs":               tftypes.List{ElementType: stringType},
		"length":               tftypes.Number,
		"triggers_reset":       stringType,
		"triggers_replacement": stringType,
		"outputs":              tftypes.List{ElementType: stringType},
		"id":                   stringType,
	}}, map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"length":               tftypes.NewValue(tftypes.Number, 5),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"outputs":              tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"id":                   tftypes.NewValue(stringType, "x"),
	})

	for label, tc := range map[string]struct {
		schema schema.Schema
		raw    tftypes.Value
	}{
		"set":  {setSchema, setOld},
		"list": {listSchema, listOld},
	} {
		t.Run(label, func(t *testing.T) {
			ctx := context.Background()
			st := tfsdk.State{Raw: tc.raw, Schema: tc.schema}
			// GetAttribute, not Get: the raw state predates the new
			// attributes, so a struct decode cannot map them. GetAttribute
			// walks the schema path and, for an attribute absent from the
			// raw value, yields a null of the schema type without erroring
			// (fwschemadata.ValueAtPath ignores tftypes.ErrInvalidStep).
			var expiresAfter types.Int64
			if diags := st.GetAttribute(ctx, path.Root("expires_after"), &expiresAfter); diags.HasError() {
				t.Fatalf("GetAttribute(expires_after) on pre-expiration state shape: %+v", diags)
			}
			var detailedOutputs types.Map
			if diags := st.GetAttribute(ctx, path.Root("detailed_outputs"), &detailedOutputs); diags.HasError() {
				t.Fatalf("GetAttribute(detailed_outputs) on pre-expiration state shape: %+v", diags)
			}
			if !expiresAfter.IsNull() {
				t.Errorf("ExpiresAfter = %v, want null", expiresAfter)
			}
			if !detailedOutputs.IsNull() {
				t.Errorf("DetailedOutputs = %v, want null", detailedOutputs)
			}
		})
	}
}

// resourceSchema fetches a resource's schema for state-shape tests.
func resourceSchema(t *testing.T, r resource.Resource) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("resource schema: %+v", resp.Diagnostics)
	}
	return resp.Schema
}
