// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestSetReadFillsNullAccumulated pins
// 2026-10-15-accumulator-delayed-for-design.md section 9 for Read: state
// written before that feature arrives with accumulated null (the framework
// fills a missing attribute with null on decode). Read must write
// accumulated as a copy of outputs, so the next plan is empty, and leave
// everything else alone.
func TestSetReadFillsNullAccumulated(t *testing.T) {
	ctx := context.Background()
	sch := resourceSchema(t, &setResource{})
	stringType := tftypes.String
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"expires_after":        tftypes.NewValue(tftypes.Number, nil),
		"delayed_for":          tftypes.NewValue(tftypes.Number, nil),
		"accumulated":          tftypes.NewValue(tftypes.Set{ElementType: stringType}, nil),
		"outputs":              tftypes.NewValue(tftypes.Set{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"detailed_outputs": tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
			"a": tftypes.NewValue(detailType, map[string]tftypes.Value{
				"expires_at": tftypes.NewValue(stringType, nil),
				"visible_at": tftypes.NewValue(stringType, nil),
			}),
		}),
		"id": tftypes.NewValue(stringType, "x"),
	})
	req := resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: sch}}
	resp := resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: sch}}
	(&setResource{}).Read(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %+v", resp.Diagnostics)
	}
	var state setResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("State.Get: %+v", diags)
	}
	got, _ := stringsFromSet(ctx, state.Accumulated)
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("accumulated after Read = %v, want %v", got, want)
	}
	out, _ := stringsFromSet(ctx, state.Outputs)
	if !reflect.DeepEqual(out, []string{"a"}) {
		t.Fatalf("outputs after Read = %v, want [a]", out)
	}
}

// TestMapReadFillsNullAccumulated pins spec section 10 for the map
// resource: state with accumulated null (hand-edited or minimal) refreshes
// with accumulated filled from outputs, values intact.
func TestMapReadFillsNullAccumulated(t *testing.T) {
	ctx := context.Background()
	sch := resourceSchema(t, &mapResource{})
	stringType := tftypes.String
	mapType := tftypes.Map{ElementType: stringType}
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(mapType, map[string]tftypes.Value{"a": tftypes.NewValue(stringType, "1")}),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"expires_after":        tftypes.NewValue(tftypes.Number, nil),
		"delayed_for":          tftypes.NewValue(tftypes.Number, nil),
		"accumulated":          tftypes.NewValue(mapType, nil),
		"outputs":              tftypes.NewValue(mapType, map[string]tftypes.Value{"a": tftypes.NewValue(stringType, "1")}),
		"detailed_outputs": tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
			"a": tftypes.NewValue(detailType, map[string]tftypes.Value{
				"expires_at": tftypes.NewValue(stringType, nil),
				"visible_at": tftypes.NewValue(stringType, nil),
			}),
		}),
		"id": tftypes.NewValue(stringType, "x"),
	})
	req := resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: sch}}
	resp := resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: sch}}
	(&mapResource{}).Read(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %+v", resp.Diagnostics)
	}
	var state mapResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("State.Get: %+v", diags)
	}
	got, _ := stringMapFromMap(ctx, state.Accumulated)
	if want := map[string]string{"a": "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("accumulated after Read = %v, want %v", got, want)
	}
	out, _ := stringMapFromMap(ctx, state.Outputs)
	if !reflect.DeepEqual(out, map[string]string{"a": "1"}) {
		t.Fatalf("outputs after Read = %v, want map[a:1]", out)
	}
}

// TestListReadFillsNullAccumulated pins
// 2026-10-15-accumulator-delayed-for-design.md section 9 for the list
// resource: pre-feature state with accumulated null refreshes with
// accumulated filled from outputs, order and duplicates intact.
func TestListReadFillsNullAccumulated(t *testing.T) {
	ctx := context.Background()
	sch := resourceSchema(t, &listResource{})
	stringType := tftypes.String
	listType := tftypes.List{ElementType: stringType}
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	nullDetail := tftypes.NewValue(detailType, map[string]tftypes.Value{
		"expires_at": tftypes.NewValue(stringType, nil),
		"visible_at": tftypes.NewValue(stringType, nil),
	})
	history := []tftypes.Value{
		tftypes.NewValue(stringType, "b"),
		tftypes.NewValue(stringType, "a"),
		tftypes.NewValue(stringType, "b"),
	}
	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(stringType, "b")}),
		"length":               tftypes.NewValue(tftypes.Number, 5),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"expires_after":        tftypes.NewValue(tftypes.Number, nil),
		"delayed_for":          tftypes.NewValue(tftypes.Number, nil),
		"accumulated":          tftypes.NewValue(listType, nil),
		"outputs":              tftypes.NewValue(listType, history),
		"detailed_outputs": tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
			"a": nullDetail,
			"b": nullDetail,
		}),
		"id": tftypes.NewValue(stringType, "x"),
	})
	req := resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: sch}}
	resp := resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: sch}}
	(&listResource{}).Read(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %+v", resp.Diagnostics)
	}
	var state listResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("State.Get: %+v", diags)
	}
	want := []string{"b", "a", "b"}
	got, _ := stringsFromList(ctx, state.Accumulated)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accumulated after Read = %v, want %v", got, want)
	}
	out, _ := stringsFromList(ctx, state.Outputs)
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("outputs after Read = %v, want %v", out, want)
	}
	if state.Length.ValueInt64() != 5 {
		t.Fatalf("length after Read = %v, want 5", state.Length)
	}
}
