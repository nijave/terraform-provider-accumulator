// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

var (
	_ resource.Resource                   = (*listResource)(nil)
	_ resource.ResourceWithImportState    = (*listResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*listResource)(nil)
	_ resource.ResourceWithValidateConfig = (*listResource)(nil)
)

// listResource accumulates inputs across applies, keeping the most recent
// length entries. State is the only store, so Delete is a no-op and Read
// only removes expired values and reveals values whose delayed_for has
// passed.
type listResource struct{}

// NewListResource returns the accumulator_list resource.
func NewListResource() resource.Resource {
	return &listResource{}
}

// listShape adapts accumulator_list to the shared phases in core.go: a list
// of strings is its own identity list, duplicates and order included.
// Import seeds both inputs and outputs from the ID.
var listShape = sliceShape(stringsFromList, listFromStrings,
	func(id string) ([]string, []string, error) {
		seed, err := accumulate.ParseImport(id)
		return seed.Inputs, seed.Outputs, err
	})

// listNext is accumulator_list's accumulation step. Append is not
// idempotent, so unlike the set and map the list compares planned inputs
// against the previous apply's (inputsChanged), and it trims to length.
func listNext(planInputs []string, inputsChanged bool, length int) nextFunc[[]string] {
	return func(reseed bool, prior []string) []string {
		return accumulate.NextListOutputs(reseed, inputsChanged, planInputs, prior, length)
	}
}

// listResourceModel is accumulator_list's state model: the shared
// attributes plus length and the three lists.
type listResourceModel struct {
	coreModel
	Inputs      types.List  `tfsdk:"inputs"`
	Length      types.Int64 `tfsdk:"length"`
	Accumulated types.List  `tfsdk:"accumulated"`
	Outputs     types.List  `tfsdk:"outputs"`
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
			"before, and re-submitting a value appends it again: there is no deduplication here. " +
			"With `expires_after` set, values that leave `inputs` expire after the TTL; see " +
			"`expires_after` for the clock rules.",
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
			"expires_after": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, maxExpiresAfter)},
				MarkdownDescription: "How many seconds a value that has left `inputs` survives in " +
					"`outputs`. While a value stays in `inputs` it never expires and its `expires_at` " +
					"is null. Expiration is realized at refresh: a value is removed when a refresh runs " +
					"after its `expires_at` has passed, and running with `-refresh=false` defers " +
					"removal until the next refresh. A late-applied saved plan will not expire an item " +
					"even if the time has passed by the time the plan is applied — its refresh already " +
					"ran at plan time; a new plan/apply is required. The entry for a value that leaves " +
					"`inputs` (or whose stamp is being recalculated after an `expires_after` change) " +
					"shows as known after apply in that apply's plan. Changing the value recalculates " +
					"every existing `expires_at` as min(old, new) on a decrease and max(old, new) on " +
					"an increase. Must be between 0 and 9223372036 (about 292 years); `0` stamps a " +
					"value with the current moment as it leaves `inputs`, so the next refresh removes " +
					"it. Leave it null to disable expiration entirely.",
			},
			"delayed_for": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, maxExpiresAfter)},
				MarkdownDescription: "How many seconds a value waits in `accumulated` before it appears in " +
					"`outputs`. The apply that first accumulates a value stamps its `visible_at` as now " +
					"plus `delayed_for` and leaves it out of `outputs`; the reveal is realized at " +
					"refresh: a value joins `outputs` when a refresh runs after its `visible_at` has " +
					"passed, and running with `-refresh=false` defers the reveal until the next refresh. " +
					"A late-applied saved plan will not reveal an item even if the time has passed by " +
					"the time the plan is applied — its refresh already ran at plan time; a new " +
					"plan/apply is required. Create, replacement, and a `triggers_reset` change delay " +
					"every seeded value. Setting `delayed_for` on an existing resource hides nothing " +
					"that is already visible. Changing the value recalculates every pending " +
					"`visible_at` as min(old, new) on a decrease and max(old, new) on an increase; " +
					"removing it reveals every pending value at that apply. Must be between 0 and " +
					"9223372036 (about 292 years) and strictly less than `expires_after` when both are " +
					"set; `0` stamps the current moment so the next refresh reveals the value. Leave it " +
					"null to disable the delay entirely.",
			},
			"accumulated": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The accumulated history, oldest first, trimmed to at most `length` " +
					"elements, including values still waiting out `delayed_for`. `outputs` is this " +
					"list minus the pending values, in the same order; with `delayed_for` null the " +
					"two are always equal. Computed; never configured. Known at plan time under the " +
					"same conditions as `outputs`.",
			},
			"detailed_outputs": schema.MapAttribute{
				Computed:    true,
				ElementType: detailedObjectType,
				MarkdownDescription: "A map from every value in `accumulated` to its timing attributes. " +
					"`expires_at` is an RFC 3339 timestamp of when the value will be removed, or null " +
					"when the value cannot expire: it is in `inputs`, or `expires_after` is null. " +
					"`visible_at` is an RFC 3339 timestamp of when a pending value will join `outputs`, " +
					"or null when it is already visible. Computed; never configured. Entries being " +
					"stamped by an apply show as known after apply in that apply's plan. Expiry and " +
					"reveal are realized at refresh; a late-applied saved plan will not expire or " +
					"reveal an item whose time passed after the plan was created — a new plan/apply is " +
					"required.",
			},
			"outputs": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The visible history, oldest first: `accumulated` minus every " +
					"value still waiting out `delayed_for`, so it may hold fewer than `length` " +
					"elements while values are pending. Computed; never configured. The plan shows " +
					"the value the next apply will produce, computed against the prior state. When " +
					"`inputs` (including any single element), `length`, a trigger, `expires_after`, " +
					"or `delayed_for` is itself unknown at plan time, `outputs` plans as unknown " +
					"and the apply resolves it.",
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

// ValidateConfig is 2026-10-15-accumulator-delayed-for-design.md C1 on raw
// configuration: delayed_for must be strictly less than expires_after when both
// are known. Unknown values skip the check here; ModifyPlan repeats it once
// they resolve.
func (r *listResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config listResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(config.DelayedFor, config.ExpiresAfter)...)
}

// Create seeds accumulated from the planned inputs through applyPhase. It
// also runs after a replacement, which is what makes triggers_replacement
// reseed.
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

	acc, out, d := applyPhase(ctx, listShape, applyCall[[]string, types.List]{
		plan:            &plan.coreModel,
		planAccumulated: plan.Accumulated,
		planOutputs:     plan.Outputs,
		planInputs:      inputs,
		next:            listNext(inputs, false, int(plan.Length.ValueInt64())),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan writes the outputs the next apply will produce into the plan
// through planPhase. length is a branch input of its own, and Terraform
// marks unknown list elements individually, so an unknown element counts
// too.
func (r *listResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan, state listResourceModel
	creating, ok := readPlanAndState(ctx, req, &plan, &state, &resp.Diagnostics)
	if !ok || listContainsUnknown(plan.Inputs) || plan.Length.IsUnknown() ||
		(!creating && state.Inputs.IsUnknown()) {
		return
	}
	inputsChanged := !creating && !plan.Inputs.Equal(state.Inputs)

	acc, out, ok, d := planPhase(ctx, listShape, planCall[[]string, types.List]{
		creating:         creating,
		plan:             &plan.coreModel,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		decodeInputs: func(ctx context.Context) ([]string, nextFunc[[]string], diag.Diagnostics) {
			in, d := stringsFromList(ctx, plan.Inputs)
			return in, listNext(in, inputsChanged, int(plan.Length.ValueInt64())), d
		},
	})
	resp.Diagnostics.Append(d...)
	if !ok || resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// Read culls expired values and reveals due ones through readPhase.
// Duplicates share a stamp, so they expire and reveal together. It writes
// state only when a value was dropped or revealed, or when state predates
// accumulated and needs it filled from outputs.
func (r *listResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state listResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	acc, out, changed, d := readPhase(ctx, listShape, &state.coreModel, state.Accumulated, state.Outputs)
	resp.Diagnostics.Append(d...)
	if !changed || resp.Diagnostics.HasError() {
		return
	}
	state.Accumulated, state.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update realizes the plan through applyPhase.
func (r *listResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	inputsChanged := !plan.Inputs.Equal(state.Inputs)

	acc, out, d := applyPhase(ctx, listShape, applyCall[[]string, types.List]{
		plan:             &plan.coreModel,
		planAccumulated:  plan.Accumulated,
		planOutputs:      plan.Outputs,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             listNext(planInputs, inputsChanged, int(plan.Length.ValueInt64())),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op. The framework removes the resource from state and there is
// nothing external to tear down.
func (r *listResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// ImportState seeds the resource from a JSON object through importPhase.
// length, triggers_reset, and triggers_replacement are left null: length is
// required configuration, so the next plan fills it in and Update re-trims
// the seeded outputs. Seeding inputs is what lets a matching configuration
// produce an empty plan instead of appending the configured list a second
// time, and id hashes the seeded inputs.
func (r *listResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seedInputs, _, outputs, detailed, d := importPhase(ctx, listShape, req.ID)
	resp.Diagnostics.Append(d...)
	inputs, d := listFromStrings(ctx, seedInputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &listResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashID(seedInputs)),
		Inputs:      inputs,
		Length:      types.Int64Null(),
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
