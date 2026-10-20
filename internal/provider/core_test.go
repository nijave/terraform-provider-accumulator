// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestModelsRoundTripThroughEmbeddedCore pins that embedding coreModel
// leaves every model's state shape unchanged: a fully populated raw state
// decodes into the model and encodes back to an identical raw value, so
// every tfsdk tag still resolves, promoted fields included.
func TestModelsRoundTripThroughEmbeddedCore(t *testing.T) {
	ctx := context.Background()
	stringType := tftypes.String
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	detailed := tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
		"a": tftypes.NewValue(detailType, map[string]tftypes.Value{
			"expires_at": tftypes.NewValue(stringType, "2000-01-01T00:00:00Z"),
			"visible_at": tftypes.NewValue(stringType, nil),
		}),
	})
	shared := map[string]tftypes.Value{
		"triggers_reset":       tftypes.NewValue(stringType, "r"),
		"triggers_replacement": tftypes.NewValue(stringType, "p"),
		"expires_after":        tftypes.NewValue(tftypes.Number, 3600),
		"delayed_for":          tftypes.NewValue(tftypes.Number, 60),
		"detailed_outputs":     detailed,
		"id":                   tftypes.NewValue(stringType, "x"),
	}
	list := tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")})
	set := tftypes.NewValue(tftypes.Set{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")})
	strMap := tftypes.NewValue(tftypes.Map{ElementType: stringType}, map[string]tftypes.Value{"a": tftypes.NewValue(stringType, "1")})

	for label, tc := range map[string]struct {
		res   resource.Resource
		own   map[string]tftypes.Value
		model func() any
	}{
		"list": {&listResource{}, map[string]tftypes.Value{
			"inputs": list, "length": tftypes.NewValue(tftypes.Number, 5), "accumulated": list, "outputs": list,
		}, func() any { return &listResourceModel{} }},
		"set": {&setResource{}, map[string]tftypes.Value{
			"inputs": list, "accumulated": set, "outputs": set,
		}, func() any { return &setResourceModel{} }},
		"map": {&mapResource{}, map[string]tftypes.Value{
			"inputs": strMap, "accumulated": strMap, "outputs": strMap,
		}, func() any { return &mapResourceModel{} }},
	} {
		t.Run(label, func(t *testing.T) {
			sch := resourceSchema(t, tc.res)
			attrs := map[string]tftypes.Value{}
			for k, v := range shared {
				attrs[k] = v
			}
			for k, v := range tc.own {
				attrs[k] = v
			}
			raw := tftypes.NewValue(sch.Type().TerraformType(ctx), attrs)
			model := tc.model()
			if diags := (tfsdk.State{Raw: raw, Schema: sch}).Get(ctx, model); diags.HasError() {
				t.Fatalf("Get: %+v", diags)
			}
			out := tfsdk.State{Raw: tftypes.NewValue(sch.Type().TerraformType(ctx), nil), Schema: sch}
			if diags := out.Set(ctx, model); diags.HasError() {
				t.Fatalf("Set: %+v", diags)
			}
			if !out.Raw.Equal(raw) {
				t.Fatalf("round trip changed the state:\n got: %s\nwant: %s", out.Raw, raw)
			}
		})
	}
}

// TestModifyPlanNullInputElementWithUnknownBranchInput pins that planned
// inputs are decoded only after the unknown guards: a known collection with a
// null element plus an unknown expires_after leaves outputs unknown and
// reports no error, as ModifyPlan did before planPhase.
func TestModifyPlanNullInputElementWithUnknownBranchInput(t *testing.T) {
	nullElem := tftypes.NewValue(goldString, nil)
	for _, f := range goldFlavors() {
		t.Run(f.name, func(t *testing.T) {
			ctx := context.Background()
			f.sch = resourceSchema(t, f.res)
			typ := f.sch.Type().TerraformType(ctx)
			var inputs tftypes.Value
			if f.name == "map" {
				inputs = tftypes.NewValue(goldMapType, map[string]tftypes.Value{"b": nullElem})
			} else {
				inputs = tftypes.NewValue(goldListType, []tftypes.Value{goldStr("a"), nullElem})
			}
			proposed := goldObject(ctx, f, map[string]tftypes.Value{
				"inputs":        inputs,
				"expires_after": goldUnknown(tftypes.Number),
			}, true, goldUnknown(goldString))
			req := resource.ModifyPlanRequest{
				Config: tfsdk.Config{Raw: proposed, Schema: f.sch},
				Plan:   tfsdk.Plan{Raw: proposed, Schema: f.sch},
				State:  tfsdk.State{Raw: tftypes.NewValue(typ, nil), Schema: f.sch},
			}
			resp := resource.ModifyPlanResponse{Plan: req.Plan}
			f.res.(resource.ResourceWithModifyPlan).ModifyPlan(ctx, req, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %s", goldDiags(resp.Diagnostics))
			}
			var attrs map[string]tftypes.Value
			if err := resp.Plan.Raw.As(&attrs); err != nil {
				t.Fatal(err)
			}
			if attrs["outputs"].IsKnown() {
				t.Fatalf("outputs = %s, want unknown", attrs["outputs"])
			}
		})
	}
}

// TestSetUpdateReportsEveryDecodeError pins that applyPhase appends the
// planned-inputs decode error and the prior-state stamp error together, as
// planPhase does, rather than stopping at the first.
func TestSetUpdateReportsEveryDecodeError(t *testing.T) {
	ctx := context.Background()
	res := &setResource{}
	sch := resourceSchema(t, res)
	str := tftypes.String
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": str, "visible_at": str}}
	detailed := tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
		"a": tftypes.NewValue(detailType, map[string]tftypes.Value{
			"expires_at": tftypes.NewValue(str, "not-a-timestamp"),
			"visible_at": tftypes.NewValue(str, nil),
		}),
	})
	listType := tftypes.List{ElementType: str}
	setType := tftypes.Set{ElementType: str}
	set := tftypes.NewValue(setType, []tftypes.Value{tftypes.NewValue(str, "a")})
	raw := func(inputs tftypes.Value) tftypes.Value {
		return tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
			"triggers_reset":       tftypes.NewValue(str, nil),
			"triggers_replacement": tftypes.NewValue(str, nil),
			"expires_after":        tftypes.NewValue(tftypes.Number, nil),
			"delayed_for":          tftypes.NewValue(tftypes.Number, nil),
			"detailed_outputs":     detailed,
			"id":                   tftypes.NewValue(str, "x"),
			"inputs":               inputs,
			"accumulated":          set,
			"outputs":              set,
		})
	}
	planInputs := tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(str, nil)})
	stateInputs := tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(str, "a")})

	req := resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: raw(planInputs), Schema: sch},
		State: tfsdk.State{Raw: raw(stateInputs), Schema: sch},
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Raw: raw(stateInputs), Schema: sch}}
	res.Update(ctx, req, &resp)

	if got := resp.Diagnostics.ErrorsCount(); got < 2 {
		t.Fatalf("got %d error diagnostics, want the inputs and the stamp error: %+v", got, resp.Diagnostics)
	}
}
