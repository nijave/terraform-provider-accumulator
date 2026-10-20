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

// mapShape adapts accumulator_map to the shared phases in core.go. The stamp
// functions run over the sorted keys, so values never reach them
// (2026-10-18-accumulator-map-design.md V3: a value change is never an
// entry), and Project re-attaches the values. Import seeds no inputs; an
// inputs key in the ID is accepted and ignored.
var mapShape = shape[map[string]string, types.Map]{
	decode:  stringMapFromMap,
	encode:  mapFromStringMap,
	keys:    accumulate.SortedKeys,
	project: accumulate.Project,
	hash:    accumulate.HashMapID,
	parse: func(id string) (map[string]string, map[string]string, error) {
		seed, err := accumulate.ParseMapImport(id)
		return nil, seed.Outputs, err
	},
}

// mapNext is accumulator_map's accumulation step: the planned inputs merged
// over the prior map. Merging is idempotent, so no inputs comparison is
// needed: merging whenever Create or Update runs is correct.
func mapNext(planInputs map[string]string) nextFunc[map[string]string] {
	return func(reseed bool, prior map[string]string) map[string]string {
		return accumulate.NextMapOutputs(reseed, planInputs, prior)
	}
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

// mapResourceModel is accumulator_map's state model: the shared attributes
// plus the three maps.
type mapResourceModel struct {
	coreModel
	Inputs      types.Map `tfsdk:"inputs"`
	Accumulated types.Map `tfsdk:"accumulated"`
	Outputs     types.Map `tfsdk:"outputs"`
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

// Create seeds accumulated from the planned inputs through applyPhase. It
// also runs after a replacement, which is what makes triggers_replacement
// reseed.
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

	acc, out, d := applyPhase(ctx, mapShape, applyCall[map[string]string, types.Map]{
		plan:            &plan.coreModel,
		planAccumulated: plan.Accumulated,
		planOutputs:     plan.Outputs,
		planInputs:      inputs,
		next:            mapNext(inputs),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan writes the outputs the next apply will produce into the plan
// through planPhase. Terraform marks unknown map values individually, so an
// unknown value counts as an unknown branch input; a map whose keys are
// unknown is wholly unknown.
func (r *mapResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan, state mapResourceModel
	creating, ok := readPlanAndState(ctx, req, &plan, &state, &resp.Diagnostics)
	if !ok || mapContainsUnknown(plan.Inputs) || (!creating && state.Inputs.IsUnknown()) {
		return
	}

	acc, out, ok, d := planPhase(ctx, mapShape, planCall[map[string]string, types.Map]{
		creating:         creating,
		plan:             &plan.coreModel,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		decodeInputs: func(ctx context.Context) (map[string]string, nextFunc[map[string]string], diag.Diagnostics) {
			in, d := stringMapFromMap(ctx, plan.Inputs)
			return in, mapNext(in), d
		},
	})
	resp.Diagnostics.Append(d...)
	if !ok || resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// Read culls expired keys and reveals due ones through readPhase. It writes
// state only when a key was dropped or revealed, or when state lacks
// accumulated and needs it filled from outputs.
func (r *mapResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mapResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	acc, out, changed, d := readPhase(ctx, mapShape, &state.coreModel, state.Accumulated, state.Outputs)
	resp.Diagnostics.Append(d...)
	if !changed || resp.Diagnostics.HasError() {
		return
	}
	state.Accumulated, state.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update realizes the plan through applyPhase.
func (r *mapResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planInputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)

	acc, out, d := applyPhase(ctx, mapShape, applyCall[map[string]string, types.Map]{
		plan:             &plan.coreModel,
		planAccumulated:  plan.Accumulated,
		planOutputs:      plan.Outputs,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             mapNext(planInputs),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
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
	// ParseMapImport returns a fresh, non-nil map, so no defensive copy is needed.
	seeded := seed.Outputs
	outputs, d := mapFromStringMap(ctx, seeded)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, accumulate.SortedKeys(seeded), stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &mapResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashMapID(seeded)),
		Inputs:      inputs,
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
