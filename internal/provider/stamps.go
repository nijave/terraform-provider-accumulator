// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

// stampArgs carries everything the stamp decisions need besides the
// accumulated result itself. On a create or reseed the caller leaves
// PriorAccumulated nil and Prior zero, so every value enters with a full
// delay window and no expiry stamp survives (spec T4).
type stampArgs struct {
	PlanInputs       []string
	PriorAccumulated []string
	Prior            stamps
	PriorExpires     *int64
	PlannedExpires   *int64
	PriorDelay       *int64
	PlannedDelay     *int64
}

// classify runs both clock-free classifications over accumulated.
func (a stampArgs) classify(accumulated []string) classified {
	var c classified
	c.KeepExpires, c.FreshExpires = accumulate.ClassifyStamps(accumulated, a.PlanInputs,
		a.Prior.Expires, a.PriorExpires, a.PlannedExpires)
	c.KeepVisible, c.FreshVisible = accumulate.ClassifyDelays(accumulated, a.PriorAccumulated,
		a.Prior.Visible, a.PriorDelay, a.PlannedDelay)
	return c
}

// planStamps is the known-path plan composition (spec section 6): outputs
// is accumulated minus pending values, and detailed_outputs carries known
// keeper stamps and unknown fresh ones. It reads no clock.
func planStamps(ctx context.Context, accumulated []string, a stampArgs) ([]string, types.Map, diag.Diagnostics) {
	c := a.classify(accumulated)
	outputs := accumulate.Visible(accumulated, c.KeepVisible, c.FreshVisible)
	detailed, diags := classifiedMap(ctx, accumulated, c)
	return outputs, detailed, diags
}

// applyStamps is the known-path apply composition: it reproduces the plan's
// classification through the same pure functions and resolves each fresh
// entry with the apply clock. Membership (outputs, accumulated) comes from
// the plan verbatim; only detailed_outputs is rebuilt here.
func applyStamps(ctx context.Context, accumulated []string, a stampArgs, now time.Time) (types.Map, diag.Diagnostics) {
	c := a.classify(accumulated)
	s := stamps{
		Expires: make(map[string]time.Time, len(c.KeepExpires)+len(c.FreshExpires)),
		Visible: make(map[string]time.Time, len(c.KeepVisible)+len(c.FreshVisible)),
	}
	for v, stamp := range c.KeepExpires {
		s.Expires[v] = stamp
	}
	for v := range c.FreshExpires {
		s.Expires[v] = accumulate.FreshStamp(a.Prior.Expires, a.PriorExpires, a.PlannedExpires, now, v)
	}
	for v, stamp := range c.KeepVisible {
		s.Visible[v] = stamp
	}
	for v := range c.FreshVisible {
		s.Visible[v] = accumulate.FreshStamp(a.Prior.Visible, a.PriorDelay, a.PlannedDelay, now, v)
	}
	return mapFromStamps(ctx, accumulated, s)
}

// applyFallback is the unknown-path apply composition: expiry culls merged
// with the apply clock, then the delay hides entering and pending values.
// It returns the new accumulated (survivors), outputs (visible survivors),
// and detailed_outputs.
func applyFallback(ctx context.Context, merged []string, a stampArgs, now time.Time) ([]string, []string, types.Map, diag.Diagnostics) {
	accumulated, expires := accumulate.ApplyExpiration(merged, a.PlanInputs,
		a.Prior.Expires, a.PriorExpires, a.PlannedExpires, now)
	outputs, visible := accumulate.ApplyDelay(accumulated, a.PriorAccumulated,
		a.Prior.Visible, a.PriorDelay, a.PlannedDelay, now)
	detailed, diags := mapFromStamps(ctx, accumulated, stamps{Expires: expires, Visible: visible})
	return accumulated, outputs, detailed, diags
}

// checkDelayShorterThanExpiry is spec C1: with both attributes known and
// non-null, delayed_for must be strictly less than expires_after, or a
// value could expire before it ever became visible. ValidateConfig runs it
// on configuration and ModifyPlan repeats it on the plan, so a pair that
// was unknown at validation still fails at plan rather than at apply.
func checkDelayShorterThanExpiry(delay, expires types.Int64) diag.Diagnostics {
	var diags diag.Diagnostics
	if delay.IsNull() || delay.IsUnknown() || expires.IsNull() || expires.IsUnknown() {
		return diags
	}
	if delay.ValueInt64() >= expires.ValueInt64() {
		diags.AddAttributeError(path.Root("delayed_for"),
			"delayed_for must be shorter than expires_after",
			fmt.Sprintf("delayed_for is %d seconds but expires_after is %d seconds; a value must "+
				"become visible before it can expire, so delayed_for must be strictly less than "+
				"expires_after.", delay.ValueInt64(), expires.ValueInt64()))
	}
	return diags
}
