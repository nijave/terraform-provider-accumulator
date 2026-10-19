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
