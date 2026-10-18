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
	_ resource.Resource                   = (*mapResource)(nil)
	_ resource.ResourceWithImportState    = (*mapResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*mapResource)(nil)
	_ resource.ResourceWithValidateConfig = (*mapResource)(nil)
)

// mapResource accumulates inputs across applies into a map keyed by the
// input keys. The key is the identity: every membership, expiry, and delay
// decision is made per key, and a re-supplied key takes the planned value
// (2026-10-18-accumulator-map-design.md section 3, overwrite in place).
// State is the only store, so Delete is a no-op and Read only ever removes
// expired keys and reveals due ones.
type mapResource struct{}

// NewMapResource returns the accumulator_map resource.
func NewMapResource() resource.Resource {
	return &mapResource{}
}

// priorAccumulated reads the store from state: accumulated, or outputs when
// state lacks it (2026-10-18-accumulator-map-design.md
// section 10).
func (m mapResourceModel) priorAccumulated(ctx context.Context) (map[string]string, diag.Diagnostics) {
	if m.Accumulated.IsNull() {
		return stringMapFromMap(ctx, m.Outputs)
	}
	return stringMapFromMap(ctx, m.Accumulated)
}

// ValidateConfig is spec C1 on raw configuration: delayed_for must be
// strictly less than expires_after when both are known. Unknown values skip
// the check here; ModifyPlan repeats it once they resolve.
func (r *mapResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config mapResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(config.DelayedFor, config.ExpiresAfter)...)
}

// mapResourceModel is accumulator_map's state model.
type mapResourceModel struct {
	Inputs              types.Map    `tfsdk:"inputs"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	DelayedFor          types.Int64  `tfsdk:"delayed_for"`
	Accumulated         types.Map    `tfsdk:"accumulated"`
	Outputs             types.Map    `tfsdk:"outputs"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
	ID                  types.String `tfsdk:"id"`
}

func (r *mapResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_map"
}

func (r *mapResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies into an `outputs` map keyed by the input " +
			"keys. Every key ever seen is retained with its most recent value: re-supplying a key with " +
			"a different value overwrites the stored value in place, and the key keeps its identity. A " +
			"key is forgotten by a `triggers_reset` change, a replacement, or — when `expires_after` is " +
			"set — expiry after the TTL; see `expires_after` for the clock rules. With `delayed_for` " +
			"set, a newly supplied key lands in `accumulated` at once but joins `outputs` only after " +
			"the delay; see `delayed_for` for the clock rules. A value change never delays or expires " +
			"a key; only key presence matters.",
		Attributes: map[string]schema.Attribute{
			"inputs": schema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The entries to accumulate. Terraform stores `inputs` in state so " +
					"the value round-trips; an update merges it into `outputs` (or, when " +
					"`triggers_reset` changes, reseeds `outputs` from it). A key already present takes " +
					"the supplied value; re-supplying a key with the same value changes nothing.",
			},
			"triggers_reset": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Change this value to discard the accumulated map and reseed " +
					"`outputs` from `inputs`, forgetting every key seen before the change. Any change " +
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
			"expires_after": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, maxExpiresAfter)},
				MarkdownDescription: "How many seconds a key that has left `inputs` survives in " +
					"`outputs`. While a key stays in `inputs` it never expires and its `expires_at` " +
					"is null, whatever happens to its value. Expiration is realized at refresh: a key is " +
					"removed when a refresh runs after its `expires_at` has passed, and running with " +
					"`-refresh=false` defers removal until the next refresh. A late-applied saved plan " +
					"will not expire an entry even if the time has passed by the time the plan is " +
					"applied — its refresh already ran at plan time; a new plan/apply is required. The " +
					"entry for a key that leaves `inputs` (or whose stamp is being recalculated after an " +
					"`expires_after` change) shows as known after apply in that apply's plan. Changing " +
					"the value recalculates every existing `expires_at` as min(old, new) on a decrease " +
					"and max(old, new) on an increase. Must be between 0 and 9223372036 (about 292 " +
					"years); `0` stamps a key with the current moment as it leaves `inputs`, so the next " +
					"refresh removes it. Leave it null to disable expiration entirely.",
			},
			"delayed_for": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, maxExpiresAfter)},
				MarkdownDescription: "How many seconds a key waits in `accumulated` before it appears in " +
					"`outputs`. The apply that first accumulates a key stamps its `visible_at` as now " +
					"plus `delayed_for` and leaves it out of `outputs`; the reveal is realized at " +
					"refresh: a key joins `outputs` when a refresh runs after its `visible_at` has " +
					"passed, and running with `-refresh=false` defers the reveal until the next refresh. " +
					"A late-applied saved plan will not reveal an entry even if the time has passed by " +
					"the time the plan is applied — its refresh already ran at plan time; a new " +
					"plan/apply is required. Create, replacement, and a `triggers_reset` change delay " +
					"every seeded key. A value change on a key that is already present never delays it. " +
					"Setting `delayed_for` on an existing resource hides nothing that is already visible. " +
					"Changing the value recalculates every pending `visible_at` as min(old, new) on a " +
					"decrease and max(old, new) on an increase; removing it reveals every pending key at " +
					"that apply. Must be between 0 and 9223372036 (about 292 years) and strictly less " +
					"than `expires_after` when both are set; `0` stamps the current moment so the next " +
					"refresh reveals the key. Leave it null to disable the delay entirely.",
			},
			"accumulated": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Every key ever accumulated with its most recent value, including " +
					"keys still waiting out `delayed_for`. `outputs` is this map minus the pending keys; " +
					"with `delayed_for` null the two are always equal. Computed; never configured. Known " +
					"at plan time under the same conditions as `outputs`.",
			},
			"detailed_outputs": schema.MapAttribute{
				Computed:    true,
				ElementType: detailedObjectType,
				MarkdownDescription: "A map from every key in `accumulated` to its timing attributes. " +
					"`expires_at` is an RFC 3339 timestamp of when the key will be removed, or null " +
					"when the key cannot expire: it is in `inputs`, or `expires_after` is null. " +
					"`visible_at` is an RFC 3339 timestamp of when a pending key will join `outputs`, " +
					"or null when it is already visible. Computed; never configured. Entries being " +
					"stamped by an apply show as known after apply in that apply's plan. Expiry and " +
					"reveal are realized at refresh; a late-applied saved plan will not expire or " +
					"reveal an entry whose time passed after the plan was created — a new plan/apply is " +
					"required.",
			},
			"outputs": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The visible entries: `accumulated` minus every key still waiting " +
					"out `delayed_for`, values carried verbatim. Computed; never configured. The plan " +
					"shows the value the next apply will produce, computed against the prior state. " +
					"When `inputs` (including any single entry's value), a trigger, `expires_after`, or " +
					"`delayed_for` is itself unknown at plan time, `outputs` plans as unknown and the " +
					"apply resolves it.",
			},
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				MarkdownDescription: "A stable identifier: the lowercase hex SHA-256 of the canonical " +
					"JSON encoding of the resource's initial values. On create (and replacement) that " +
					"is the `inputs` map; on import it is the seeded `outputs`, because a map resource " +
					"seeds no meaningful `inputs`. Stable across in-place updates.",
			},
		},
	}
}

// Create seeds accumulated from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
//
// Known path: ModifyPlan already computed accumulated, outputs,
// detailed_outputs, and the id without reading the clock; membership is
// realized verbatim and only the fresh visible_at entries are resolved
// with the apply clock. Fallback path (unknown branch inputs at plan time):
// create is the reseed case, every key comes from inputs, and both stamps
// come from the apply clock through applyFallback.
func (r *mapResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create is the reseed case: no prior store, no prior stamps, so every
	// key enters (2026-10-15-accumulator-delayed-for-design.md T4) and the only
	// clock use is stamping visible_at.
	args := stampArgs{
		PlanInputs:     accumulate.SortedKeys(inputs),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// Known path: realize membership verbatim and resolve the fresh
		// visible_at entries with the apply clock.
		accumulated, d := stringMapFromMap(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulate.SortedKeys(accumulated), args, clockNow())
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextMapOutputs(true, inputs, nil)
	survivors, visible, detailed, d := applyFallback(ctx, accumulate.SortedKeys(merged), args, clockNow())
	resp.Diagnostics.Append(d...)
	acc, d := mapFromStringMap(ctx, accumulate.Project(merged, survivors))
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(merged, visible))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashMapID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan writes the outputs the next apply will produce into the plan.
// It runs the same accumulation algorithm as Create and Update, through the
// same accumulate.NextMapOutputs call, so the planned value cannot disagree
// with the applied one. It reads no clock: fresh detailed_outputs entries
// plan as unknown and the apply stamps them. When any value the branch
// decision depends on is still unknown, outputs is left unknown and the
// apply resolves it.
func (r *mapResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// A destroy plan carries no planned state to compute from, and the
	// framework requires it to stay null.
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// State is null when the resource is being created, and Get refuses a null
	// state, so it is only read when there is one to read.
	creating := req.State.Raw.IsNull()

	var state mapResourceModel
	if !creating {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// An unknown branch input makes the planned outputs unknowable, not merely
	// unresolved: a triggers_reset that may have changed yields either the
	// reseeded map or the merged one, and no single planned value can express
	// that. Leaving outputs untouched keeps the framework's unknown marking in
	// place until Create or Update resolves the value. Unknown values inside
	// inputs count too: Terraform marks them individually.
	if mapContainsUnknown(plan.Inputs) || plan.TriggersReset.IsUnknown() ||
		plan.TriggersReplacement.IsUnknown() || plan.ExpiresAfter.IsUnknown() ||
		plan.DelayedFor.IsUnknown() {
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

	planInputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	var priorAccumulated map[string]string
	var prior stamps
	args := stampArgs{
		PlanInputs:     accumulate.SortedKeys(planInputs),
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
	// which is what reseed expresses. No inputs comparison is needed, matching
	// Update: merging is idempotent, so merging whenever Create or Update runs
	// is correct.
	replacing := !creating && !plan.TriggersReplacement.Equal(state.TriggersReplacement)
	reseed := creating || replacing || !plan.TriggersReset.Equal(state.TriggersReset)

	merged := accumulate.NextMapOutputs(reseed, planInputs, priorAccumulated)

	// A reseed discards history: no prior store and no prior stamps, so
	// every key enters with a full delay window (2026-10-15-accumulator-delayed-for-design.md
	// T4).
	if !reseed {
		args.PriorAccumulated = accumulate.SortedKeys(priorAccumulated)
		args.Prior = prior
	}

	// No clock here (2026-10-18-accumulator-map-design.md section
	// 6): refresh already culled and revealed, so membership is a pure
	// function of state and configuration.
	visible, detailed, d := planStamps(ctx, accumulate.SortedKeys(merged), args)
	resp.Diagnostics.Append(d...)
	acc, d := mapFromStringMap(ctx, merged)
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(merged, visible))
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
		plan.ID = types.StringValue(accumulate.HashMapID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// Read culls expired keys and reveals due ones: the refresh-time half of
// both designs. Read cannot see configuration, so both decisions run on
// stored stamps alone. It writes state only when a key was dropped or
// revealed, or when state lacks accumulated and needs it filled from
// outputs, which keeps the plan after a refresh empty.
func (r *mapResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mapResourceModel
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

	keys := accumulate.SortedKeys(values)
	now := clockNow()
	survivors, expires := accumulate.FilterExpired(keys, prior.Expires, now)
	visible, pending := accumulate.FilterPending(survivors, prior.Visible, now)
	if !state.Accumulated.IsNull() && len(survivors) == len(keys) && len(pending) == len(prior.Visible) {
		return
	}

	acc, d := mapFromStringMap(ctx, accumulate.Project(values, survivors))
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(values, visible))
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

// Update realizes the plan. On the known path every time-dependent decision
// was already made by ModifyPlan (membership and keeper stamps), so Update
// reproduces them through the same pure functions and resolves only the
// fresh entries (unknown in the plan) with the apply clock. The fallback
// path runs only when the plan is unknown, where the plan promised unknown
// and Update is free to decide with the apply clock through applyFallback.
func (r *mapResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringMapFromMap(ctx, plan.Inputs)
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
		PlanInputs:     accumulate.SortedKeys(planInputs),
		PriorExpires:   int64Ptr(state.ExpiresAfter),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PriorDelay:     int64Ptr(state.DelayedFor),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if !reseed {
		args.PriorAccumulated = accumulate.SortedKeys(priorAccumulated)
		args.Prior = prior
	}
	now := clockNow()

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// Known path: membership and keeper stamps reproduce exactly through
		// the pure functions ModifyPlan used; only fresh entries resolve here.
		accumulated, d := stringMapFromMap(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulate.SortedKeys(accumulated), args, now)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextMapOutputs(reseed, planInputs, priorAccumulated)
	survivors, visible, detailed, d := applyFallback(ctx, accumulate.SortedKeys(merged), args, now)
	resp.Diagnostics.Append(d...)
	acc, d := mapFromStringMap(ctx, accumulate.Project(merged, survivors))
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(merged, visible))
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

// Delete is a no-op. The framework removes the resource from state and there
// is nothing external to tear down.
func (r *mapResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// ImportState seeds the map from a JSON object. id hashes the seeded
// outputs. inputs is seeded as an empty map rather than null: it is a
// required attribute, and an empty map is a valid non-null value. The next
// plan sets inputs from configuration and Update merges it into the seeded
// outputs. An inputs key in the ID is accepted and ignored.
func (r *mapResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseMapImport(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := mapFromStringMap(ctx, map[string]string{})
	resp.Diagnostics.Append(d...)
	seeded := accumulate.MergeMap(nil, seed.Outputs)
	outputs, d := mapFromStringMap(ctx, seeded)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, accumulate.SortedKeys(seeded), stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &mapResourceModel{
		Inputs:              inputs,
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		DelayedFor:          types.Int64Null(),
		Accumulated:         outputs,
		Outputs:             outputs,
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(accumulate.HashMapID(seeded)),
	})...)
}
