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
	_ resource.Resource                   = (*setResource)(nil)
	_ resource.ResourceWithImportState    = (*setResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*setResource)(nil)
	_ resource.ResourceWithValidateConfig = (*setResource)(nil)
)

// setResource accumulates inputs across applies into a deduplicated set.
// State is the only store, so Delete is a no-op and Read only removes
// expired values and reveals values whose delayed_for has passed.
type setResource struct{}

// NewSetResource returns the accumulator_set resource.
func NewSetResource() resource.Resource {
	return &setResource{}
}

// setShape adapts accumulator_set to the shared phases in core.go: a set of
// strings is its own identity list. Import deduplicates the seeded outputs
// and seeds no inputs; an inputs key in the ID is accepted and ignored.
var setShape = sliceShape(stringsFromSet, setFromStrings,
	func(id string) ([]string, []string, error) {
		seed, err := accumulate.ParseImport(id)
		if err != nil {
			return nil, nil, err
		}
		return nil, accumulate.Union(nil, seed.Outputs), nil
	})

// priorAccumulated reads the store from state: accumulated, or outputs when
// state predates the attribute (2026-10-15-accumulator-delayed-for-design.md
// section 9).
func (m setResourceModel) priorAccumulated(ctx context.Context) ([]string, diag.Diagnostics) {
	if m.Accumulated.IsNull() {
		return stringsFromSet(ctx, m.Outputs)
	}
	return stringsFromSet(ctx, m.Accumulated)
}

// setNext is accumulator_set's accumulation step: a union of the planned
// inputs into the prior set. Union is idempotent, so no inputs comparison is
// needed: unioning whenever Create or Update runs is correct.
func setNext(planInputs []string) nextFunc[[]string] {
	return func(reseed bool, prior []string) []string {
		return accumulate.NextSetOutputs(reseed, planInputs, prior)
	}
}

// ValidateConfig is 2026-10-15-accumulator-delayed-for-design.md C1 on raw
// configuration: delayed_for must be strictly less than expires_after when both
// are known. Unknown values skip the check here; ModifyPlan repeats it once
// they resolve.
func (r *setResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config setResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(config.DelayedFor, config.ExpiresAfter)...)
}

// setResourceModel is accumulator_set's state model: the shared attributes
// plus inputs (a list, the shape users write) and the two sets.
type setResourceModel struct {
	coreModel
	Inputs      types.List `tfsdk:"inputs"`
	Accumulated types.Set  `tfsdk:"accumulated"`
	Outputs     types.Set  `tfsdk:"outputs"`
}

func (r *setResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_set"
}

func (r *setResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies into a deduplicated `outputs` set. " +
			"Every value ever seen is retained once, and a value is forgotten by a " +
			"`triggers_reset` change, a replacement, or — when `expires_after` is set — " +
			"expiry after the TTL; see `expires_after` for the clock rules. With " +
			"`delayed_for` set, a newly supplied value lands in `accumulated` at once " +
			"but joins `outputs` only after the delay; see `delayed_for` for the clock " +
			"rules. `inputs` is a " +
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
			"accumulated": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Every value ever accumulated, each appearing once, including " +
					"values still waiting out `delayed_for`. `outputs` is this set minus the pending " +
					"values; with `delayed_for` null the two are always equal. Computed; never " +
					"configured. Known at plan time under the same conditions as `outputs`.",
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
			"outputs": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The visible values: `accumulated` minus every value still waiting " +
					"out `delayed_for`, each appearing once. Computed; " +
					"never configured. The plan shows the value the next apply will produce, " +
					"computed against the prior state. When `inputs` (including any single " +
					"element), a trigger, `expires_after`, or `delayed_for` is itself unknown " +
					"at plan time, `outputs` plans as unknown and the apply resolves it.",
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

	// Create is the reseed case: no prior store, no prior stamps, so every
	// value enters (2026-10-15-accumulator-delayed-for-design.md T4) and the
	// only clock use is stamping visible_at.
	args := stampArgs{
		PlanInputs:     inputs,
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// Known path: realize membership verbatim and resolve the fresh
		// visible_at entries with the apply clock.
		accumulated, d := stringsFromSet(ctx, plan.Accumulated)
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

	merged := accumulate.NextSetOutputs(true, inputs, nil)
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, clockNow())
	resp.Diagnostics.Append(d...)
	acc, d := setFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := setFromStrings(ctx, outputs)
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

// ModifyPlan writes the outputs the next apply will produce into the plan
// through planPhase. inputs is a list, and Terraform marks unknown list
// elements individually, so an unknown element counts as an unknown branch
// input.
func (r *setResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan, state setResourceModel
	creating, ok := readPlanAndState(ctx, req, &plan, &state, &resp.Diagnostics)
	if !ok || listContainsUnknown(plan.Inputs) || (!creating && state.Inputs.IsUnknown()) {
		return
	}

	acc, out, ok, d := planPhase(ctx, setShape, planCall[[]string, types.Set]{
		creating:         creating,
		plan:             &plan.coreModel,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		decodeInputs: func(ctx context.Context) ([]string, nextFunc[[]string], diag.Diagnostics) {
			in, d := stringsFromList(ctx, plan.Inputs)
			return in, setNext(in), d
		},
	})
	resp.Diagnostics.Append(d...)
	if !ok || resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// Read culls expired values and reveals due ones through readPhase. It
// writes state only when a value was dropped or revealed, or when state
// predates accumulated and needs it filled from outputs.
func (r *setResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state setResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	acc, out, changed, d := readPhase(ctx, setShape, &state.coreModel, state.Accumulated, state.Outputs)
	resp.Diagnostics.Append(d...)
	if !changed || resp.Diagnostics.HasError() {
		return
	}
	state.Accumulated, state.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update realizes the plan. On the known path every time-dependent decision was
// already made by ModifyPlan (membership and keeper stamps), so Update
// reproduces them through the same pure functions and resolves only the fresh
// entries (unknown in the plan) with the apply clock
// (2026-10-15-accumulator-delayed-for-design.md section 6). The fallback path
// runs only when the plan is unknown (unknown branch inputs at plan time),
// where the plan promised unknown and Update is free to decide with the apply
// clock through applyFallback. No inputs comparison is needed: union is
// idempotent.
func (r *setResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state setResourceModel
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
		accumulated, d := stringsFromSet(ctx, plan.Accumulated)
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

	merged := accumulate.NextSetOutputs(reseed, planInputs, priorAccumulated)
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, now)
	resp.Diagnostics.Append(d...)
	acc, d := setFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := setFromStrings(ctx, outputs)
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
	// {"outputs":["a","a","b"]} then yields the same id as
	// {"outputs":["a","b"]}, matching the state it produces
	// (2026-09-17-terraform-provider-accumulator-design.md section 8).
	merged := accumulate.Union(nil, seed.Outputs)
	outputs, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	detailed, d := mapFromStamps(ctx, merged, stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &setResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashID(merged)),
		Inputs:      inputs,
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
