// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

var (
	_ resource.Resource                = (*listResource)(nil)
	_ resource.ResourceWithImportState = (*listResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*listResource)(nil)
)

// listResource accumulates inputs across applies, keeping the most recent
// length entries. State is the only store, so Read and Delete are no-ops.
type listResource struct{}

// NewListResource returns the accumulator_list resource.
func NewListResource() resource.Resource {
	return &listResource{}
}

// listResourceModel is accumulator_list's state model.
type listResourceModel struct {
	Inputs              types.List   `tfsdk:"inputs"`
	Length              types.Int64  `tfsdk:"length"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	Outputs             types.List   `tfsdk:"outputs"`
	ID                  types.String `tfsdk:"id"`
}

func (r *listResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_list"
}

func (r *listResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies, keeping the most recent `length` " +
			"entries in `outputs`. Each apply whose `inputs` differs from the previous apply appends " +
			"the whole new list; changing only `length` re-trims the existing history without " +
			"appending. A change to `inputs` is treated as new input even when it overlaps what came " +
			"before, and re-submitting a value appends it again: there is no deduplication here.",
		Attributes: map[string]schema.Attribute{
			"inputs": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The values to accumulate. Terraform stores `inputs` in state so " +
					"the provider can compare the planned list against the previous apply's list.",
			},
			"length": schema.Int64Attribute{
				Required:   true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
				MarkdownDescription: "How many of the most recent values to keep in `outputs`. " +
					"`length = 0` produces an empty `outputs`. Must be zero or greater.",
			},
			"triggers_reset": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Change this value to discard the accumulated history and reseed " +
					"`outputs` from `inputs`. Any change counts, including from null to a value. Leave " +
					"it null for no reset.",
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
			"outputs": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The accumulated history, oldest first, trimmed to at most " +
					"`length` elements. Computed; never configured. The plan shows the value the " +
					"next apply will produce, computed against the prior state. When `inputs` " +
					"(including any single element), `length`, or a trigger is itself unknown at " +
					"plan time, `outputs` plans as unknown and the apply resolves it.",
			},
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				MarkdownDescription: "A stable identifier: the lowercase hex SHA-256 of the canonical " +
					"JSON encoding of the `inputs` present when the resource was created. Stable across " +
					"in-place updates; recomputed on replacement.",
			},
		},
	}
}

// Create seeds outputs from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
func (r *listResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan listResourceModel
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
	outputs, d := listFromStrings(ctx,
		accumulate.NextListOutputs(true, false, inputs, nil, int(plan.Length.ValueInt64())))
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
// the same accumulate.NextListOutputs call, so the planned value cannot
// disagree with the applied one. When any value the branch decision depends on
// is still unknown, outputs is left unknown and the apply resolves it.
func (r *listResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// A destroy plan carries no planned state to compute from, and the
	// framework requires it to stay null.
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// State is null when the resource is being created, and Get refuses a null
	// state, so it is only read when there is one to read.
	creating := req.State.Raw.IsNull()

	var state listResourceModel
	var stateOutputs []string
	if !creating {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// An unknown branch input makes the planned outputs unknowable, not merely
	// unresolved: a triggers_reset that may have changed yields either the
	// reseeded list or the appended one, and no single planned value can
	// express that. Leaving outputs untouched keeps the framework's unknown
	// marking in place until Create or Update resolves the value. Unknown
	// elements inside inputs count too: Terraform marks them individually.
	if listContainsUnknown(plan.Inputs) || plan.Length.IsUnknown() ||
		plan.TriggersReset.IsUnknown() || plan.TriggersReplacement.IsUnknown() {
		return
	}
	if !creating && (state.Inputs.IsUnknown() || state.Outputs.IsUnknown() ||
		state.TriggersReset.IsUnknown() || state.TriggersReplacement.IsUnknown()) {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if !creating {
		stateOutputs, d = stringsFromList(ctx, state.Outputs)
		resp.Diagnostics.Append(d...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	// The branch conditions mirror Update's, with two additions: a null state
	// means Create runs, and a triggers_replacement change means a replacement
	// means Create runs. Both seed outputs from the planned inputs alone,
	// which is what reseed expresses.
	replacing := !creating && !plan.TriggersReplacement.Equal(state.TriggersReplacement)
	reseed := creating || replacing || !plan.TriggersReset.Equal(state.TriggersReset)
	inputsChanged := !creating && !plan.Inputs.Equal(state.Inputs)

	outputs := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, stateOutputs, int(plan.Length.ValueInt64()))

	out, d := listFromStrings(ctx, outputs)
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

// Read is a structural no-op. The framework copies the prior state into the
// Read response before calling Read, so returning it unchanged means a refresh
// can never perturb an attribute. That is what makes a second plan after a
// successful apply empty.
func (r *listResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

// Update applies the accumulation algorithm from spec section 6.1.
//
// triggers_replacement is never inspected here: a change to it plans a
// replacement, and Update is not called.
func (r *listResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	stateOutputs, d := stringsFromList(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	length := int(plan.Length.ValueInt64())

	// The branch order is pinned by accumulate.NextListOutputs: reset wins over
	// an inputs change, and an inputs change wins over a length-only re-trim.
	reseed := !plan.TriggersReset.Equal(state.TriggersReset)
	inputsChanged := !plan.Inputs.Equal(state.Inputs)
	outputs := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, stateOutputs, length)

	out, d := listFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op. The framework removes the resource from state and there is
// nothing external to tear down.
func (r *listResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// ImportState seeds the resource from a JSON object. length, triggers_reset,
// and triggers_replacement are left null: length is required configuration, so
// the next plan fills it in and Update re-trims the seeded outputs. Seeding
// inputs is what lets a matching configuration produce an empty plan instead of
// appending the configured list a second time.
func (r *listResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseImport(req.ID)
	if err != nil {
		// The error names the expected shape and quotes the offending key only.
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := listFromStrings(ctx, seed.Inputs)
	resp.Diagnostics.Append(d...)
	outputs, d := listFromStrings(ctx, seed.Outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &listResourceModel{
		Inputs:              inputs,
		Length:              types.Int64Null(),
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		Outputs:             outputs,
		ID:                  types.StringValue(accumulate.HashID(seed.Inputs)),
	})...)
}
