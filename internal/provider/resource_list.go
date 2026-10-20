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

// priorAccumulated reads the store from state: accumulated, or outputs when
// state predates the attribute (2026-10-15-accumulator-delayed-for-design.md
// section 9).
func (m listResourceModel) priorAccumulated(ctx context.Context) ([]string, diag.Diagnostics) {
	if m.Accumulated.IsNull() {
		return stringsFromList(ctx, m.Outputs)
	}
	return stringsFromList(ctx, m.Accumulated)
}

// listResourceModel is accumulator_list's state model.
type listResourceModel struct {
	Inputs              types.List   `tfsdk:"inputs"`
	Length              types.Int64  `tfsdk:"length"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	DelayedFor          types.Int64  `tfsdk:"delayed_for"`
	Accumulated         types.List   `tfsdk:"accumulated"`
	Outputs             types.List   `tfsdk:"outputs"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
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

// Create seeds accumulated from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
//
// Known path: ModifyPlan already computed accumulated, outputs,
// detailed_outputs, and the id without reading the clock
// (2026-10-15-accumulator-delayed-for-design.md section 6); membership is
// realized verbatim and only the fresh visible_at entries are resolved with the
// apply clock, which is what keeps the applied value identical to the planned
// one. Fallback path (unknown branch inputs at plan time): create is the reseed
// case, every value comes from inputs, and both stamps come from the apply
// clock through applyFallback.
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

	// Create is the reseed case: no prior store, no prior stamps, so every
	// value enters (2026-10-15-accumulator-delayed-for-design.md T4) and the
	// only clock use is stamping visible_at.
	args := stampArgs{
		PlanInputs:     inputs,
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		accumulated, d := stringsFromList(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulated, args, clockNow())
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextListOutputs(true, false, inputs, nil, int(plan.Length.ValueInt64()))
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, clockNow())
	resp.Diagnostics.Append(d...)
	acc, d := listFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan writes the outputs the next apply will produce into the plan, so
// the plan shows concrete values and downstream resources can plan against
// them. It runs the same accumulation algorithm as Create and Update, through
// the same accumulate.NextListOutputs call, so the planned value cannot
// disagree with the applied one. It reads no clock
// (2026-10-15-accumulator-delayed-for-design.md section 6): fresh
// detailed_outputs entries plan as unknown and the apply stamps them. When any
// value the branch decision depends on is still unknown, outputs is left
// unknown and the apply resolves it.
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
		plan.TriggersReset.IsUnknown() || plan.TriggersReplacement.IsUnknown() ||
		plan.ExpiresAfter.IsUnknown() || plan.DelayedFor.IsUnknown() {
		return
	}
	if !creating && (state.Inputs.IsUnknown() || state.Outputs.IsUnknown() ||
		state.TriggersReset.IsUnknown() || state.TriggersReplacement.IsUnknown() ||
		state.ExpiresAfter.IsUnknown() || state.DetailedOutputs.IsUnknown() ||
		state.DelayedFor.IsUnknown() || state.Accumulated.IsUnknown()) {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(plan.DelayedFor, plan.ExpiresAfter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	var priorAccumulated []string
	var prior stamps
	args := stampArgs{
		PlanInputs:     planInputs,
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if !creating {
		priorAccumulated, d = state.priorAccumulated(ctx)
		resp.Diagnostics.Append(d...)
		prior, d = stampsFromMap(ctx, state.DetailedOutputs)
		resp.Diagnostics.Append(d...)
		args.PriorExpires = int64Ptr(state.ExpiresAfter)
		args.PriorDelay = int64Ptr(state.DelayedFor)
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

	merged := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, priorAccumulated,
		int(plan.Length.ValueInt64()))

	// A reseed discards history: no prior store and no prior stamps, so every
	// value enters with a full delay window
	// (2026-10-15-accumulator-delayed-for-design.md T4).
	if !reseed {
		args.PriorAccumulated = priorAccumulated
		args.Prior = prior
	}

	// No clock here (2026-10-15-accumulator-delayed-for-design.md section 6):
	// refresh already culled and revealed, so membership is a pure function of
	// state and configuration.
	outputs, detailed, d := planStamps(ctx, merged, args)
	resp.Diagnostics.Append(d...)
	acc, d := listFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	if creating || replacing {
		// Create identifies the resource from the planned inputs. An in-place
		// update keeps the state id; UseStateForUnknown has already resolved
		// it into the plan by the time ModifyPlan runs.
		plan.ID = types.StringValue(accumulate.HashID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// Read culls expired values and reveals due ones: the refresh-time half of both
// designs. Read cannot see configuration, so both decisions run on stored
// stamps alone. It writes state only when a value was dropped or revealed, or
// when state predates accumulated and needs it filled from outputs
// (2026-10-15-accumulator-delayed-for-design.md section 9), which keeps the
// plan after a refresh empty.
func (r *listResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state listResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values, d := state.priorAccumulated(ctx)
	resp.Diagnostics.Append(d...)
	prior, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	now := clockNow()
	survivors, expires := accumulate.FilterExpired(values, prior.Expires, now)
	visible, pending := accumulate.FilterPending(survivors, prior.Visible, now)
	if !state.Accumulated.IsNull() && len(survivors) == len(values) && len(pending) == len(prior.Visible) {
		return
	}

	acc, d := listFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, visible)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps{Expires: expires, Visible: pending})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Accumulated = acc
	state.Outputs = out
	state.DetailedOutputs = detailed
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update realizes the plan. On the known path every time-dependent decision was
// already made by ModifyPlan (membership and keeper stamps), so Update
// reproduces them through the same pure functions and resolves only the fresh
// entries (unknown in the plan) with the apply clock
// (2026-10-15-accumulator-delayed-for-design.md section 6). The fallback path
// runs only when the plan is unknown (unknown branch inputs at plan time),
// where the plan promised unknown and Update is free to decide with the apply
// clock through applyFallback.
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
	priorAccumulated, d := state.priorAccumulated(ctx)
	resp.Diagnostics.Append(d...)
	prior, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	reseed := !plan.TriggersReset.Equal(state.TriggersReset)
	args := stampArgs{
		PlanInputs:     planInputs,
		PriorExpires:   int64Ptr(state.ExpiresAfter),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PriorDelay:     int64Ptr(state.DelayedFor),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if !reseed {
		args.PriorAccumulated = priorAccumulated
		args.Prior = prior
	}
	now := clockNow()

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// Known path: membership and keeper stamps reproduce exactly through
		// the pure functions ModifyPlan used; only fresh entries resolve here.
		accumulated, d := stringsFromList(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulated, args, now)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	inputsChanged := !plan.Inputs.Equal(state.Inputs)
	merged := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, priorAccumulated,
		int(plan.Length.ValueInt64()))
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, now)
	resp.Diagnostics.Append(d...)
	acc, d := listFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
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

	detailed, d := mapFromStamps(ctx, seed.Outputs, stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &listResourceModel{
		Inputs:              inputs,
		Length:              types.Int64Null(),
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		DelayedFor:          types.Int64Null(),
		Accumulated:         outputs,
		Outputs:             outputs,
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(accumulate.HashID(seed.Inputs)),
	})...)
}
