// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

var (
	_ resource.Resource                = (*setResource)(nil)
	_ resource.ResourceWithImportState = (*setResource)(nil)
)

// setResource accumulates inputs across applies into a deduplicated set. State
// is the only store, so Read and Delete are no-ops.
type setResource struct{}

// NewSetResource returns the accumulator_set resource.
func NewSetResource() resource.Resource {
	return &setResource{}
}

// setResourceModel is accumulator_set's state model.
type setResourceModel struct {
	Inputs              types.List   `tfsdk:"inputs"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	Outputs             types.Set    `tfsdk:"outputs"`
	ID                  types.String `tfsdk:"id"`
}

func (r *setResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_set"
}

func (r *setResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies into a deduplicated `outputs` set. " +
			"Every value ever seen is retained once, and a value is only forgotten by a " +
			"`triggers_reset` change or a replacement. `inputs` is a list, matching the shape users " +
			"write in configuration. `outputs` is a set, so Terraform does not preserve or promise any " +
			"ordering of its elements; only the membership is meaningful.",
		Attributes: map[string]schema.Attribute{
			"inputs": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The values to accumulate. Terraform stores `inputs` in state so " +
					"the value round-trips; an update unions it into `outputs` (or, when " +
					"`triggers_reset` changes, reseeds `outputs` from it). Re-submitting a value that " +
					"is already present changes nothing.",
			},
			"triggers_reset": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Change this value to discard the accumulated set and reseed " +
					"`outputs` from `inputs`, forgetting every value seen before the change. Any change " +
					"counts, including from null to a value. Leave it null for no reset.",
			},
			"triggers_replacement": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				MarkdownDescription: "Change this value to force a replacement (destroy and recreate), " +
					"which reseeds `outputs` from `inputs`. Any change counts, including from null to a " +
					"value. Leave it null for no replacement.",
			},
			"outputs": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Every value ever accumulated, each appearing once. Computed; " +
					"never configured. Deliberately has no `UseStateForUnknown`: it must plan as unknown " +
					"whenever an input changes, because the applied value depends on the prior state.",
			},
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				MarkdownDescription: "A stable identifier: the lowercase hex SHA-256 of the canonical " +
					"JSON encoding of the resource's initial values. On create (and replacement) that " +
					"is the `inputs` list; on import it is the seeded `outputs`, because a set resource " +
					"seeds no meaningful `inputs`. Stable across in-place updates.",
			},
		},
	}
}

// Create seeds outputs from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
func (r *setResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	outputs, d := setFromStrings(ctx, accumulate.Merge(nil, inputs))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = outputs
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read is a structural no-op, for the same reason as accumulator_list's: the
// framework copies prior state into the response, so a refresh cannot perturb
// an attribute.
func (r *setResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

// Update applies the accumulation algorithm from spec section 6.2. No inputs
// comparison is needed: union is idempotent, so applying it whenever Update
// runs is correct, and Update does not run when nothing changed.
func (r *setResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	stateOutputs, d := stringsFromSet(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	var merged []string
	if !plan.TriggersReset.Equal(state.TriggersReset) {
		// Reset: discard history and reseed from the planned inputs.
		merged = accumulate.Merge(nil, planInputs)
	} else {
		merged = accumulate.Merge(stateOutputs, planInputs)
	}

	outputs, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = outputs
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op. The framework removes the resource from state and there is
// nothing external to tear down.
func (r *setResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// ImportState seeds the set from a JSON object. outputs is deduplicated and
// id hashes the seeded outputs. inputs is seeded as an empty list rather than
// null: it is a required attribute, and an empty list is a valid non-null
// value, so nothing depends on tolerating a null required attribute in
// imported state. The next plan sets inputs from configuration and Update
// unions it into the seeded outputs. An inputs key in the ID is accepted and
// ignored.
func (r *setResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseImport(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := listFromStrings(ctx, []string{})
	resp.Diagnostics.Append(d...)
	// Hash the deduplicated outputs, not the raw seed: a seed such as
	// {"outputs":["a","a","b"]} then yields the same id as {"outputs":["a","b"]},
	// matching the state it produces (design spec section 8).
	merged := accumulate.Merge(nil, seed.Outputs)
	outputs, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &setResourceModel{
		Inputs:              inputs,
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		Outputs:             outputs,
		ID:                  types.StringValue(accumulate.HashID(merged)),
	})...)
}
