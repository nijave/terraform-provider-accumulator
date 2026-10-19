// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
