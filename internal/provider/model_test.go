// SPDX-License-Identifier: MPL-2.0

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
// Every caller passes accumulate.Union's output, which is unique by
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

	values := []string{"a", "b", "c", "d"}
	in := stamps{
		Expires: map[string]time.Time{"b": base.Add(5 * time.Minute), "d": base.Add(time.Hour)},
		Visible: map[string]time.Time{"c": base.Add(time.Minute), "d": base.Add(30 * time.Second)},
	}
	m, diags := mapFromStamps(ctx, values, in)
	if diags.HasError() {
		t.Fatalf("mapFromStamps: %+v", diags)
	}
	if m.IsNull() || m.IsUnknown() {
		t.Fatalf("mapFromStamps returned %v; it must be concrete", m)
	}
	if got := len(m.Elements()); got != 4 {
		t.Fatalf("map has %d entries, want 4", got)
	}

	parsed, diags := stampsFromMap(ctx, m)
	if diags.HasError() {
		t.Fatalf("stampsFromMap: %+v", diags)
	}
	if !reflect.DeepEqual(parsed, in) {
		t.Fatalf("round trip = %+v, want %+v", parsed, in)
	}
	// The all-null entry ("a") must not appear in either map: absence is
	// the null stamp.
	if _, ok := parsed.Expires["a"]; ok {
		t.Fatal("null expires_at round-tripped as a stamp")
	}
	if _, ok := parsed.Visible["a"]; ok {
		t.Fatal("null visible_at round-tripped as a stamp")
	}
}

func TestMapFromStampsIsNeverNull(t *testing.T) {
	m, diags := mapFromStamps(context.Background(), nil, stamps{})
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

// TestClassifiedMap pins the plan-shape split for both stamp kinds: keepers
// carry the prior stamp, fresh entries are unknown (the apply stamps them),
// and everything else is null. The map itself stays known.
func TestClassifiedMap(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	m, diags := classifiedMap(ctx, []string{"a", "b", "c", "d"}, classified{
		KeepExpires:  map[string]time.Time{"b": base.Add(5 * time.Minute)},
		FreshExpires: map[string]struct{}{"c": {}},
		KeepVisible:  map[string]time.Time{"c": base.Add(time.Minute)},
		FreshVisible: map[string]struct{}{"d": {}},
	})
	if diags.HasError() {
		t.Fatalf("classifiedMap: %+v", diags)
	}
	if m.IsNull() || m.IsUnknown() {
		t.Fatalf("classifiedMap returned %v; it must be a concrete map", m)
	}

	elems := m.Elements()
	if got := len(elems); got != 4 {
		t.Fatalf("map has %d entries, want 4", got)
	}
	attrOf := func(v, name string) types.String {
		t.Helper()
		obj, ok := elems[v].(types.Object)
		if !ok {
			t.Fatalf("entry %q is %T, want types.Object", v, elems[v])
		}
		s, ok := obj.Attributes()[name].(types.String)
		if !ok {
			t.Fatalf("entry %q %s is %T, want types.String", v, name, obj.Attributes()[name])
		}
		return s
	}

	if s := attrOf("a", "expires_at"); !s.IsNull() {
		t.Errorf("a expires_at = %v, want null", s)
	}
	if s := attrOf("a", "visible_at"); !s.IsNull() {
		t.Errorf("a visible_at = %v, want null", s)
	}
	if s := attrOf("b", "expires_at"); s.ValueString() != base.Add(5*time.Minute).Format(time.RFC3339) {
		t.Errorf("b expires_at = %q, want the kept stamp", s.ValueString())
	}
	if s := attrOf("c", "expires_at"); !s.IsUnknown() {
		t.Errorf("c expires_at = %v, want unknown", s)
	}
	if s := attrOf("c", "visible_at"); s.ValueString() != base.Add(time.Minute).Format(time.RFC3339) {
		t.Errorf("c visible_at = %q, want the kept stamp", s.ValueString())
	}
	if s := attrOf("d", "visible_at"); !s.IsUnknown() {
		t.Errorf("d visible_at = %v, want unknown", s)
	}
	if s := attrOf("d", "expires_at"); !s.IsNull() {
		t.Errorf("d expires_at = %v, want null", s)
	}
}

func TestStampsFromMapNullAndUnknownAreEmpty(t *testing.T) {
	for _, m := range []types.Map{types.MapNull(detailedObjectType), types.MapUnknown(detailedObjectType)} {
		got, diags := stampsFromMap(context.Background(), m)
		if diags.HasError() {
			t.Fatalf("stampsFromMap(%v): %+v", m, diags)
		}
		if len(got.Expires) != 0 || len(got.Visible) != 0 {
			t.Fatalf("stampsFromMap(%v) = %+v, want empty maps", m, got)
		}
	}
}

func TestStampsFromMapRejectsGarbageTimestamp(t *testing.T) {
	for _, name := range []string{"expires_at", "visible_at"} {
		attrs := map[string]attr.Value{
			"expires_at": types.StringNull(),
			"visible_at": types.StringNull(),
		}
		attrs[name] = types.StringValue("not-a-timestamp")
		m := types.MapValueMust(detailedObjectType, map[string]attr.Value{
			"b": types.ObjectValueMust(detailedObjectType.AttrTypes, attrs),
		})
		if _, diags := stampsFromMap(context.Background(), m); !diags.HasError() {
			t.Fatalf("stampsFromMap accepted a malformed %s; it must diagnose", name)
		}
	}
}

// TestStampsFromMapToleratesMissingVisibleAt pins spec section 9: an entry
// written before visible_at existed has no such attribute at all. The
// framework fills a missing object attribute with null on decode, but the
// accessor must also tolerate a raw object that lacks the key.
func TestStampsFromMapToleratesMissingVisibleAt(t *testing.T) {
	oldType := types.ObjectType{AttrTypes: map[string]attr.Type{"expires_at": types.StringType}}
	m := types.MapValueMust(oldType, map[string]attr.Value{
		"b": types.ObjectValueMust(oldType.AttrTypes, map[string]attr.Value{
			"expires_at": types.StringValue("2026-09-23T10:05:00Z"),
		}),
	})
	got, diags := stampsFromMap(context.Background(), m)
	if diags.HasError() {
		t.Fatalf("stampsFromMap on a pre-delay entry: %+v", diags)
	}
	if len(got.Visible) != 0 {
		t.Fatalf("Visible = %v, want empty", got.Visible)
	}
	if _, ok := got.Expires["b"]; !ok {
		t.Fatal("expires_at of the pre-delay entry was lost")
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

// TestStateGetToleratesMissingDelayAttributes pins spec section 9: state
// written by the pre-delay provider lacks expires_after, detailed_outputs,
// accumulated, and delayed_for entirely, and Get must unmarshal it with all
// of them null rather than erroring. If this test fails, the correct fix is
// an UpgradeState implementation, not a schema change.
func TestStateGetToleratesMissingDelayAttributes(t *testing.T) {
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
		schema          schema.Schema
		raw             tftypes.Value
		accumulatedType tftypes.Type
	}{
		"set":  {setSchema, setOld, tftypes.Set{ElementType: stringType}},
		"list": {listSchema, listOld, tftypes.List{ElementType: stringType}},
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
				t.Fatalf("GetAttribute(expires_after) on pre-delay state shape: %+v", diags)
			}
			var detailedOutputs types.Map
			if diags := st.GetAttribute(ctx, path.Root("detailed_outputs"), &detailedOutputs); diags.HasError() {
				t.Fatalf("GetAttribute(detailed_outputs) on pre-delay state shape: %+v", diags)
			}
			if !expiresAfter.IsNull() {
				t.Errorf("ExpiresAfter = %v, want null", expiresAfter)
			}
			if !detailedOutputs.IsNull() {
				t.Errorf("DetailedOutputs = %v, want null", detailedOutputs)
			}
			var delayedFor types.Int64
			if diags := st.GetAttribute(ctx, path.Root("delayed_for"), &delayedFor); diags.HasError() {
				t.Fatalf("GetAttribute(delayed_for) on pre-delay state shape: %+v", diags)
			}
			if !delayedFor.IsNull() {
				t.Errorf("DelayedFor = %v, want null", delayedFor)
			}
			// accumulated is a set on accumulator_set and a list on
			// accumulator_list, so the decode target changes with the label.
			switch label {
			case "set":
				var accumulated types.Set
				if diags := st.GetAttribute(ctx, path.Root("accumulated"), &accumulated); diags.HasError() {
					t.Fatalf("GetAttribute(accumulated) on pre-delay state shape: %+v", diags)
				}
				assertNullAccumulated(t, accumulated, tc.accumulatedType)
			case "list":
				var accumulated types.List
				if diags := st.GetAttribute(ctx, path.Root("accumulated"), &accumulated); diags.HasError() {
					t.Fatalf("GetAttribute(accumulated) on pre-delay state shape: %+v", diags)
				}
				assertNullAccumulated(t, accumulated, tc.accumulatedType)
			}
		})
	}
}

// assertNullAccumulated asserts the decoded accumulated is a null of the
// expected underlying tftypes type: the missing attribute came back as the
// schema's null, not an error or a concrete empty value.
func assertNullAccumulated(t *testing.T, accumulated attr.Value, wantType tftypes.Type) {
	t.Helper()
	if !accumulated.IsNull() {
		t.Errorf("Accumulated = %v, want null", accumulated)
	}
	raw, err := accumulated.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatalf("ToTerraformValue on the decoded accumulated: %v", err)
	}
	if want := tftypes.NewValue(wantType, nil); !raw.Equal(want) {
		t.Errorf("decoded accumulated raw value = %s, want %s", raw, want)
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

func TestMapFromStringMapIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string]map[string]string{
		"nil":    nil,
		"empty":  {},
		"values": {"a": "1", "b": ""},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := mapFromStringMap(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("mapFromStringMap: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("mapFromStringMap produced a null map; state must always hold a concrete value")
			}
			if got.IsUnknown() {
				t.Fatal("mapFromStringMap produced an unknown map")
			}
			if got.ElementType(context.Background()) != types.StringType {
				t.Fatalf("element type = %v, want string", got.ElementType(context.Background()))
			}
		})
	}
}

func TestStringMapFromMapRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	want := map[string]string{"a": "1", "b": ""}
	m, diags := mapFromStringMap(ctx, want)
	if diags.HasError() {
		t.Fatalf("mapFromStringMap: %v", diags.Errors())
	}
	got, diags := stringMapFromMap(ctx, m)
	if diags.HasError() {
		t.Fatalf("stringMapFromMap: %v", diags.Errors())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}

func TestStringMapFromMapNullAndUnknownAreNil(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for label, m := range map[string]types.Map{
		"null":    types.MapNull(types.StringType),
		"unknown": types.MapUnknown(types.StringType),
	} {
		m := m
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := stringMapFromMap(ctx, m)
			if diags.HasError() {
				t.Fatalf("stringMapFromMap: %v", diags.Errors())
			}
			if got != nil {
				t.Fatalf("stringMapFromMap(%s) = %v, want nil", label, got)
			}
		})
	}
}

func TestMapContainsUnknown(t *testing.T) {
	t.Parallel()
	known := types.MapValueMust(types.StringType, map[string]attr.Value{"a": types.StringValue("1")})
	partial := types.MapValueMust(types.StringType, map[string]attr.Value{
		"a": types.StringValue("1"), "b": types.StringUnknown(),
	})
	for label, tc := range map[string]struct {
		m    types.Map
		want bool
	}{
		"known":           {known, false},
		"null":            {types.MapNull(types.StringType), false},
		"whole unknown":   {types.MapUnknown(types.StringType), true},
		"element unknown": {partial, true},
	} {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			if got := mapContainsUnknown(tc.m); got != tc.want {
				t.Fatalf("mapContainsUnknown = %v, want %v", got, tc.want)
			}
		})
	}
}
