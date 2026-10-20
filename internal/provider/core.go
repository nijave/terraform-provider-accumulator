// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

// coreModel holds the attributes the three resources share by name, type,
// and meaning. Each resource model embeds it by value; the framework
// promotes the embedded fields, so the tfsdk tags and the state shape are
// those of the flat models it replaced.
type coreModel struct {
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	DelayedFor          types.Int64  `tfsdk:"delayed_for"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
	ID                  types.String `tfsdk:"id"`
}

// importedCore is the coreModel of freshly imported state: no triggers, no
// expiry, no delay, the given detailed_outputs, and the given id.
func importedCore(detailed types.Map, id string) coreModel {
	return coreModel{
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		DelayedFor:          types.Int64Null(),
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(id),
	}
}

// shape adapts one collection type to the shared phases. C is the Go value
// the pure functions in internal/accumulate take ([]string or
// map[string]string); V is the framework type accumulated and outputs are
// stored as (types.List, types.Set, or types.Map). Each resource declares
// one shape value; nothing in it reads or writes a model.
type shape[C any, V attr.Value] struct {
	// decode reads accumulated or outputs. Null and unknown read as empty.
	decode func(context.Context, V) (C, diag.Diagnostics)
	// encode writes a collection as a concrete, never-null V.
	encode func(context.Context, C) (V, diag.Diagnostics)
	// keys is the identity list the stamp functions run over: the values
	// themselves for a list or set, the sorted keys for a map.
	keys func(C) []string
	// project maps a key list returned by a stamp function back onto c:
	// the keys themselves for a list or set, the entries of c with those
	// keys for a map.
	project func(c C, keys []string) C
	// hash is the id of a collection: accumulate.HashID or HashMapID.
	hash func(C) string
	// parse reads an import ID into the seeded inputs and outputs. A
	// resource that seeds no inputs returns nil for them.
	parse func(id string) (inputs, outputs C, err error)
}

// sliceShape fills the parts of a shape that are the same for the list and
// the set: a []string is its own identity list.
func sliceShape[V attr.Value](decode func(context.Context, V) ([]string, diag.Diagnostics),
	encode func(context.Context, []string) (V, diag.Diagnostics),
	parse func(id string) (inputs, outputs []string, err error)) shape[[]string, V] {
	return shape[[]string, V]{
		decode:  decode,
		encode:  encode,
		keys:    func(values []string) []string { return values },
		project: func(_ []string, keys []string) []string { return keys },
		hash:    accumulate.HashID,
		parse:   parse,
	}
}

// priorAccumulated reads the store from state: accumulated, or outputs when
// state predates the attribute (2026-10-15-accumulator-delayed-for-design.md
// section 9, 2026-10-18-accumulator-map-design.md section 10).
func priorAccumulated[C any, V attr.Value](ctx context.Context, s shape[C, V], accumulated, outputs V) (C, diag.Diagnostics) {
	if accumulated.IsNull() {
		return s.decode(ctx, outputs)
	}
	return s.decode(ctx, accumulated)
}

// readPhase is Read's shared body: it culls expired values and reveals due
// ones, the refresh-time half of both designs. Read cannot see
// configuration, so both decisions run on stored stamps alone. changed is
// false when nothing was dropped or revealed and state already holds
// accumulated; the caller then writes nothing, which keeps the plan after a
// refresh empty. On change it rewrites m.DetailedOutputs and returns the new
// collections.
func readPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], m *coreModel, storedAccumulated, storedOutputs V) (accumulated, outputs V, changed bool, diags diag.Diagnostics) {
	values, d := priorAccumulated(ctx, s, storedAccumulated, storedOutputs)
	diags.Append(d...)
	prior, d := stampsFromMap(ctx, m.DetailedOutputs)
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	keys := s.keys(values)
	now := clockNow()
	survivors, expires := accumulate.FilterExpired(keys, prior.Expires, now)
	visible, pending := accumulate.FilterPending(survivors, prior.Visible, now)
	if !storedAccumulated.IsNull() && len(survivors) == len(keys) && len(pending) == len(prior.Visible) {
		return accumulated, outputs, false, diags
	}

	accumulated, d = s.encode(ctx, s.project(values, survivors))
	diags.Append(d...)
	outputs, d = s.encode(ctx, s.project(values, visible))
	diags.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps{Expires: expires, Visible: pending})
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}
	m.DetailedOutputs = detailed
	return accumulated, outputs, true, diags
}

// configUnknown reports whether a shared branch input is unknown in a plan:
// a trigger, expires_after, or delayed_for. Each resource adds its own
// terms (inputs, and the list's length).
func (c coreModel) configUnknown() bool {
	return c.TriggersReset.IsUnknown() || c.TriggersReplacement.IsUnknown() ||
		c.ExpiresAfter.IsUnknown() || c.DelayedFor.IsUnknown()
}

// stateUnknown is configUnknown for prior state, which also mirrors the
// computed detailed_outputs. State never holds unknowns in practice; the
// guard keeps ModifyPlan from computing against one if it ever did.
func (c coreModel) stateUnknown() bool {
	return c.configUnknown() || c.DetailedOutputs.IsUnknown()
}

// nextFunc is a resource's accumulation step: the next accumulated value
// from the prior one, with the planned inputs already bound. reseed
// discards prior (a create, a replacement, or a triggers_reset change).
type nextFunc[C any] func(reseed bool, prior C) C

// newStampArgs builds the stamp decision inputs for planInputs. state is
// nil on create, where nothing is prior. A reseed keeps the prior
// expires_after and delayed_for (they still decide whether a stamp is
// recalculated) but discards the prior store and stamps, so every value
// enters with a full delay window
// (2026-10-15-accumulator-delayed-for-design.md T4).
func newStampArgs(planInputs []string, plan, state *coreModel, priorKeys []string, prior stamps, reseed bool) stampArgs {
	args := stampArgs{
		PlanInputs:     planInputs,
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if state != nil {
		args.PriorExpires = int64Ptr(state.ExpiresAfter)
		args.PriorDelay = int64Ptr(state.DelayedFor)
	}
	if !reseed {
		args.PriorAccumulated = priorKeys
		args.Prior = prior
	}
	return args
}

// readPlanAndState is ModifyPlan's opening, shared by every resource. It
// decodes the plan, and the state unless the resource is being created
// (state is null then, and Get refuses a null state). ok is false on a
// destroy plan, which carries no planned state to compute from and must
// stay null, and on a decode error.
func readPlanAndState[M any](ctx context.Context, req resource.ModifyPlanRequest, plan, state *M, diags *diag.Diagnostics) (creating, ok bool) {
	if req.Plan.Raw.IsNull() {
		return false, false
	}
	diags.Append(req.Plan.Get(ctx, plan)...)
	if diags.HasError() {
		return false, false
	}
	creating = req.State.Raw.IsNull()
	if !creating {
		diags.Append(req.State.Get(ctx, state)...)
		if diags.HasError() {
			return creating, false
		}
	}
	return creating, true
}

// planCall is one ModifyPlan call's per-resource input to planPhase.
type planCall[C any, V attr.Value] struct {
	// creating is true when there is no prior state.
	creating bool
	// plan is the planned shared attributes; planPhase writes its
	// detailed_outputs and, on create or replacement, its id.
	plan *coreModel
	// state is the prior shared attributes; ignored when creating.
	state *coreModel
	// stateAccumulated and stateOutputs are the prior collections.
	stateAccumulated, stateOutputs V
	// decodeInputs decodes the planned inputs and binds them into the
	// accumulation step. planPhase calls it after the unknown guards and the
	// delay check, so an input that cannot decode (a null element) is
	// reported only where a plan would otherwise be computed from it.
	decodeInputs func(ctx context.Context) (planInputs C, next nextFunc[C], diags diag.Diagnostics)
}

// planPhase is ModifyPlan's shared body. It writes the outputs the next apply
// will produce into the plan, so the plan shows concrete values and
// downstream resources can plan against them. It runs the same
// accumulation (call.next) as Create and Update, so the planned value cannot
// disagree with the applied one. It reads no clock
// (2026-10-15-accumulator-delayed-for-design.md section 6): fresh
// detailed_outputs entries plan as unknown and the apply stamps them.
//
// ok is false when the plan must be left as the framework built it: an
// unknown branch input makes the planned outputs unknowable, not merely
// unresolved, because a triggers_reset that may have changed yields either
// the reseeded collection or the accumulated one, and no single planned
// value can express that. Leaving outputs untouched keeps the framework's
// unknown marking in place until Create or Update resolves the value. The
// caller has already checked its own terms (inputs, and the list's length).
// ok is also false on an error.
func planPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], call planCall[C, V]) (accumulated, outputs V, ok bool, diags diag.Diagnostics) {
	if call.plan.configUnknown() {
		return accumulated, outputs, false, diags
	}
	if !call.creating && (call.state.stateUnknown() ||
		call.stateAccumulated.IsUnknown() || call.stateOutputs.IsUnknown()) {
		return accumulated, outputs, false, diags
	}
	diags.Append(checkDelayShorterThanExpiry(call.plan.DelayedFor, call.plan.ExpiresAfter)...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	planInputs, next, d := call.decodeInputs(ctx)
	diags.Append(d...)

	var prior C
	var priorStamps stamps
	var state *coreModel
	if !call.creating {
		state = call.state
		prior, d = priorAccumulated(ctx, s, call.stateAccumulated, call.stateOutputs)
		diags.Append(d...)
		priorStamps, d = stampsFromMap(ctx, call.state.DetailedOutputs)
		diags.Append(d...)
	}
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	// The branch conditions mirror Update's, with two additions: a null state
	// means Create runs, and a triggers_replacement change means a
	// replacement, which also runs Create. Both seed from the planned inputs
	// alone, which is what reseed expresses.
	replacing := !call.creating && !call.plan.TriggersReplacement.Equal(call.state.TriggersReplacement)
	reseed := call.creating || replacing || !call.plan.TriggersReset.Equal(call.state.TriggersReset)

	merged := next(reseed, prior)
	var priorKeys []string
	if !reseed {
		priorKeys = s.keys(prior)
	}
	args := newStampArgs(s.keys(planInputs), call.plan, state, priorKeys, priorStamps, reseed)

	// No clock here (2026-10-15-accumulator-delayed-for-design.md section 6):
	// refresh already culled and revealed, so membership is a pure function of
	// state and configuration.
	visible, detailed, d := planStamps(ctx, s.keys(merged), args)
	diags.Append(d...)
	accumulated, d = s.encode(ctx, merged)
	diags.Append(d...)
	outputs, d = s.encode(ctx, s.project(merged, visible))
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	call.plan.DetailedOutputs = detailed
	if call.creating || replacing {
		// Create identifies the resource from the planned inputs. An in-place
		// update keeps the state id; UseStateForUnknown has already resolved
		// it into the plan by the time ModifyPlan runs.
		call.plan.ID = types.StringValue(s.hash(planInputs))
	}
	return accumulated, outputs, true, diags
}

// applyCall is one Create or Update call's per-resource input to
// applyPhase.
type applyCall[C any, V attr.Value] struct {
	// plan is the planned shared attributes; applyPhase writes its
	// detailed_outputs and id.
	plan *coreModel
	// planAccumulated and planOutputs are the planned collections.
	planAccumulated, planOutputs V
	// state is the prior shared attributes, nil for Create.
	state *coreModel
	// stateAccumulated and stateOutputs are the prior collections; ignored
	// for Create.
	stateAccumulated, stateOutputs V
	// planInputs is the decoded planned inputs.
	planInputs C
	// next computes the next accumulated value.
	next nextFunc[C]
}

// applyPhase is the shared body of Create and Update. Create runs it with a
// nil state: that is the reseed case, with no prior store and no prior
// stamps, so every value enters (2026-10-15-accumulator-delayed-for-design.md
// T4). It also runs after a replacement, which is what makes
// triggers_replacement reseed. triggers_replacement is never inspected for
// Update: a change to it plans a replacement, and Update is not called.
//
// Known path: ModifyPlan already computed accumulated, outputs,
// detailed_outputs, and the id without reading the clock
// (2026-10-15-accumulator-delayed-for-design.md section 6). Membership is
// realized verbatim (the returned collections are the planned ones), and
// the stamp classification is reproduced through the same pure functions,
// so only the fresh entries (unknown in the plan) are resolved, with the
// apply clock. That is what keeps the applied value identical to the
// planned one.
//
// Fallback path: the plan is unknown (an unknown branch input at plan
// time), the plan promised unknown, and the apply decides membership and
// both stamps with its own clock through applyFallback.
func applyPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], call applyCall[C, V]) (accumulated, outputs V, diags diag.Diagnostics) {
	var prior C
	var priorStamps stamps
	reseed := true
	if call.state != nil {
		var d diag.Diagnostics
		prior, d = priorAccumulated(ctx, s, call.stateAccumulated, call.stateOutputs)
		diags.Append(d...)
		priorStamps, d = stampsFromMap(ctx, call.state.DetailedOutputs)
		diags.Append(d...)
		if diags.HasError() {
			return accumulated, outputs, diags
		}
		reseed = !call.plan.TriggersReset.Equal(call.state.TriggersReset)
	}
	var priorKeys []string
	if !reseed {
		priorKeys = s.keys(prior)
	}
	args := newStampArgs(s.keys(call.planInputs), call.plan, call.state, priorKeys, priorStamps, reseed)
	now := clockNow()

	if !call.planOutputs.IsUnknown() && !call.planAccumulated.IsUnknown() && !call.plan.DetailedOutputs.IsUnknown() {
		planned, d := s.decode(ctx, call.planAccumulated)
		diags.Append(d...)
		detailed, d := applyStamps(ctx, s.keys(planned), args, now)
		diags.Append(d...)
		if diags.HasError() {
			return accumulated, outputs, diags
		}
		call.plan.DetailedOutputs = detailed
		if call.state != nil {
			call.plan.ID = call.state.ID
		}
		return call.planAccumulated, call.planOutputs, diags
	}

	merged := call.next(reseed, prior)
	survivors, visible, detailed, d := applyFallback(ctx, s.keys(merged), args, now)
	diags.Append(d...)
	accumulated, d = s.encode(ctx, s.project(merged, survivors))
	diags.Append(d...)
	outputs, d = s.encode(ctx, s.project(merged, visible))
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, diags
	}
	call.plan.DetailedOutputs = detailed
	if call.state != nil {
		call.plan.ID = call.state.ID
	} else {
		call.plan.ID = types.StringValue(s.hash(call.planInputs))
	}
	return accumulated, outputs, diags
}
