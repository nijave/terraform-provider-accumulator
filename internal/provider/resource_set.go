// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"time"

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
	_ resource.Resource                = (*setResource)(nil)
	_ resource.ResourceWithImportState = (*setResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*setResource)(nil)
)

// setResource accumulates inputs across applies into a deduplicated set.
// State is the only store, so Delete is a no-op and Read only ever removes
// expired values.
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
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	Outputs             types.Set    `tfsdk:"outputs"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
	ID                  types.String `tfsdk:"id"`
}

func (r *setResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_set"
}

func (r *setResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies into a deduplicated `outputs` set. " +
			"Every value ever seen is retained once, and a value is forgotten by a " +
			"`triggers_reset` change, a replacement, or — when `expires_after` is set — " +
			"expiry after the TTL; see `expires_after` for the clock rules. `inputs` is a " +
			"list, matching the shape users write in configuration. `outputs` is a set, so " +
			"Terraform does not preserve or promise any ordering of its elements; only the " +
			"membership is meaningful.",
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
			"detailed_outputs": schema.MapAttribute{
				Computed:    true,
				ElementType: detailedObjectType,
				MarkdownDescription: "A map from every value in `outputs` to its expiration attributes. " +
					"The single attribute, `expires_at`, is an RFC 3339 timestamp of when the value " +
					"will be removed, or null when the value cannot expire: it is in `inputs`, or " +
					"`expires_after` is null. Computed; never configured. Entries being stamped by an " +
					"apply show as known after apply in that apply's plan. Expiration is realized at " +
					"refresh; a late-applied saved plan will not expire an item whose time passed " +
					"after the plan was created — a new plan/apply is required.",
			},
			"outputs": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Every value ever accumulated, each appearing once. Computed; " +
					"never configured. The plan shows the value the next apply will produce, " +
					"computed against the prior state. When `inputs` (including any single " +
					"element), a trigger, or `expires_after` is itself unknown at plan time, " +
					"`outputs` plans as unknown and the apply resolves it.",
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
//
// Known path: ModifyPlan already computed outputs, detailed_outputs, and
// the id without reading the clock (spec section 6); realizing the plan
// verbatim is what keeps the applied value identical to the planned one.
// Fallback path (unknown inputs at plan time): create is the reseed case,
// every value comes from inputs, so ApplyExpiration stamps nothing and the
// clock never participates; it is still run so both paths shape the result
// the same way.
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

	if !plan.Outputs.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextSetOutputs(true, inputs, nil)
	survivors, stamps := accumulate.ApplyExpiration(merged, inputs, nil, nil,
		int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan writes the outputs the next apply will produce into the plan, so
// the plan shows concrete values and downstream resources can plan against
// them. It runs the same accumulation algorithm as Create and Update, through
// the same accumulate.NextSetOutputs call, so the planned value cannot
// disagree with the applied one. It reads no clock (spec section 6): fresh
// detailed_outputs entries plan as unknown and the apply stamps them. When any
// value the branch decision depends on is still unknown, outputs is left
// unknown and the apply resolves it.
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
	var priorStamps map[string]time.Time
	var priorExpiresAfter *int64
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
		plan.TriggersReplacement.IsUnknown() || plan.ExpiresAfter.IsUnknown() {
		return
	}
	if !creating && (state.Inputs.IsUnknown() || state.Outputs.IsUnknown() ||
		state.TriggersReset.IsUnknown() || state.TriggersReplacement.IsUnknown() ||
		state.ExpiresAfter.IsUnknown() || state.DetailedOutputs.IsUnknown()) {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if !creating {
		stateOutputs, d = stringsFromSet(ctx, state.Outputs)
		resp.Diagnostics.Append(d...)
		priorStamps, d = stampsFromMap(ctx, state.DetailedOutputs)
		resp.Diagnostics.Append(d...)
		priorExpiresAfter = int64Ptr(state.ExpiresAfter)
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

	merged := accumulate.NextSetOutputs(reseed, planInputs, stateOutputs)

	// No clock here (spec section 6): refresh already culled expired
	// values, so membership is a pure function of state and configuration,
	// and OpenTofu's apply-time re-derivation of this plan reproduces it
	// exactly. ClassifyStamps decides which detailed_outputs entries are
	// known (prior stamps under an unchanged expires_after) and which stay
	// unknown until the apply stamps them (S1, S3, S4, S5).
	keep, fresh := accumulate.ClassifyStamps(merged, planInputs, priorStamps,
		priorExpiresAfter, int64Ptr(plan.ExpiresAfter))

	out, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	detailed, d := classifiedMap(ctx, merged, keep, fresh)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

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

// Read culls expired values: the refresh-time half of the expiration
// design (spec section 6), and the per-value analogue of time_rotating's
// Read removing the resource once its rotation deadline passes. Read
// cannot see configuration, so the cull runs on stored stamps alone;
// values in inputs are safe because they never carry a stamp (R2). When
// nothing expired, the method returns without touching the response,
// which keeps the old no-op behavior for everything else: refresh only ever
// removes expired values, and only when at least one is expired.
func (r *setResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state setResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values, d := stringsFromSet(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	stamps, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	survivors, kept := accumulate.FilterExpired(values, stamps, clockNow())
	if len(survivors) == len(values) {
		return
	}

	out, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, kept)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Outputs = out
	state.DetailedOutputs = detailed
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update realizes the plan. On the known path every time-dependent decision
// was already made by ModifyPlan (membership and keeper stamps), so Update
// reproduces them through the same pure functions and resolves only the
// fresh entries (unknown in the plan) with the apply clock (spec section 6).
// The fallback path runs only when the plan is unknown (unknown branch
// inputs at plan time), where the plan promised unknown and Update is free
// to decide with the apply clock through ApplyExpiration. No inputs
// comparison is needed: union is idempotent.
func (r *setResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Outputs.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// The plan's membership and keeper stamps reproduce exactly through
		// the same pure functions ModifyPlan used; only the fresh entries
		// (unknown in the plan) are resolved here with the apply clock
		// (spec section 6).
		planInputs, d := stringsFromList(ctx, plan.Inputs)
		resp.Diagnostics.Append(d...)
		planOutputs, d := stringsFromSet(ctx, plan.Outputs)
		resp.Diagnostics.Append(d...)
		priorStamps, d := stampsFromMap(ctx, state.DetailedOutputs)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		priorE, plannedE := int64Ptr(state.ExpiresAfter), int64Ptr(plan.ExpiresAfter)
		keep, fresh := accumulate.ClassifyStamps(planOutputs, planInputs, priorStamps, priorE, plannedE)
		stamps := make(map[string]time.Time, len(keep)+len(fresh))
		for v, stamp := range keep {
			stamps[v] = stamp
		}
		now := clockNow()
		for v := range fresh {
			stamps[v] = accumulate.FreshStamp(priorStamps, priorE, plannedE, now, v)
		}
		detailed, d := mapFromStamps(ctx, planOutputs, stamps)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	stateOutputs, d := stringsFromSet(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	priorStamps, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	reseed := !plan.TriggersReset.Equal(state.TriggersReset)
	merged := accumulate.NextSetOutputs(reseed, planInputs, stateOutputs)
	survivors, stamps := accumulate.ApplyExpiration(merged, planInputs, priorStamps,
		int64Ptr(state.ExpiresAfter), int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
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

	detailed, d := mapFromStamps(ctx, merged, nil)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &setResourceModel{
		Inputs:              inputs,
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		Outputs:             outputs,
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(accumulate.HashID(merged)),
	})...)
}
