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
	_ resource.ResourceWithModifyPlan  = (*setResource)(nil)
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
					"never configured. The plan shows the value the next apply will produce, " +
					"computed against the prior state. When `inputs` (including any single element) " +
					"or a trigger is itself unknown at plan time, `outputs` plans as unknown and " +
					"the apply resolves it.",
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

	// Create is the reseed case of the shared branch decision, so the applied
	// value has one source on both the plan and apply paths.
	outputs, d := setFromStrings(ctx, accumulate.NextSetOutputs(true, inputs, nil))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = outputs
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan writes the outputs the next apply will produce into the plan, so
// the plan shows concrete values and downstream resources can plan against
// them. It runs the same accumulation algorithm as Create and Update, through
// the same accumulate.NextSetOutputs call, so the planned value cannot
// disagree with the applied one. When any value the branch decision depends on
// is still unknown, outputs is left unknown and the apply resolves it.
func (r *setResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// A destroy plan carries no planned state to compute from, and the
	// framework requires it to stay null.
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// State is null when the resource is being created, and Get refuses a null
	// state, so it is only read when there is one to read.
	creating := req.State.Raw.IsNull()

	var state setResourceModel
	var stateOutputs []string
	if !creating {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// An unknown branch input makes the planned outputs unknowable, not merely
	// unresolved: a triggers_reset that may have changed yields either the
	// reseeded set or the unioned one, and no single planned value can express
	// that. Leaving outputs untouched keeps the framework's unknown marking in
	// place until Create or Update resolves the value. Unknown elements inside
	// inputs count too: Terraform marks them individually.
	if listContainsUnknown(plan.Inputs) || plan.TriggersReset.IsUnknown() ||
		plan.TriggersReplacement.IsUnknown() {
		return
	}
	if !creating && (state.Inputs.IsUnknown() || state.Outputs.IsUnknown() ||
		state.TriggersReset.IsUnknown() || state.TriggersReplacement.IsUnknown()) {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if !creating {
		stateOutputs, d = stringsFromSet(ctx, state.Outputs)
		resp.Diagnostics.Append(d...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// The branch conditions mirror Update's, with two additions: a null state
	// means Create runs, and a triggers_replacement change means a replacement
	// means Create runs. Both seed outputs from the planned inputs alone,
	// which is what reseed expresses. No inputs comparison is needed, matching
	// Update: union is idempotent, so unioning whenever Create or Update runs
	// is correct.
	replacing := !creating && !plan.TriggersReplacement.Equal(state.TriggersReplacement)
	reseed := creating || replacing || !plan.TriggersReset.Equal(state.TriggersReset)

	outputs := accumulate.NextSetOutputs(reseed, planInputs, stateOutputs)

	out, d := setFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	if creating || replacing {
		// Create identifies the resource from the planned inputs. An in-place
		// update keeps the state id; UseStateForUnknown has already resolved
		// it into the plan by the time ModifyPlan runs.
		plan.ID = types.StringValue(accumulate.HashID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
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

	reseed := !plan.TriggersReset.Equal(state.TriggersReset)
	merged := accumulate.NextSetOutputs(reseed, planInputs, stateOutputs)

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
