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

var _ resource.Resource = (*listResource)(nil)

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
					"`length` elements. Computed; never configured. Deliberately has no " +
					"`UseStateForUnknown`: it must plan as unknown whenever an input changes, because " +
					"the applied value depends on the prior state.",
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

	outputs, d := listFromStrings(ctx, accumulate.Trim(inputs, int(plan.Length.ValueInt64())))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = outputs
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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

	var outputs []string
	switch {
	case !plan.TriggersReset.Equal(state.TriggersReset):
		// Reset: discard history and reseed from the planned inputs.
		outputs = accumulate.Trim(planInputs, length)
	case !plan.Inputs.Equal(state.Inputs):
		// Inputs changed: append the whole new list to the prior history.
		outputs = accumulate.Append(stateOutputs, planInputs, length)
	default:
		// Only length changed: re-trim the existing history.
		outputs = accumulate.Trim(stateOutputs, length)
	}

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
