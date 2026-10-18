// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func seconds(n int64) *int64 { return &n }

var stampBase = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

// TestPlanStamps pins the clock-free plan composition: outputs is the
// visible subset, keepers carry known stamps, fresh entries are unknown.
func TestPlanStamps(t *testing.T) {
	ctx := context.Background()
	args := stampArgs{
		PlanInputs:       []string{"a", "c"},
		PriorAccumulated: []string{"a", "b"},
		Prior: stamps{
			Expires: map[string]time.Time{},
			Visible: map[string]time.Time{"b": stampBase.Add(time.Hour)},
		},
		PriorExpires:   seconds(7200),
		PlannedExpires: seconds(7200),
		PriorDelay:     seconds(3600),
		PlannedDelay:   seconds(3600),
	}
	outputs, detailed, diags := planStamps(ctx, []string{"a", "b", "c"}, args)
	if diags.HasError() {
		t.Fatalf("planStamps: %+v", diags)
	}
	// b is pending (kept), c is entering (fresh): both hidden.
	if want := []string{"a"}; !reflect.DeepEqual(outputs, want) {
		t.Fatalf("outputs = %v, want %v", outputs, want)
	}
	elems := detailed.Elements()
	b := elems["b"].(types.Object).Attributes()
	if s := b["visible_at"].(types.String); s.ValueString() != stampBase.Add(time.Hour).Format(time.RFC3339) {
		t.Errorf("b visible_at = %v, want the kept stamp", s)
	}
	// b left inputs with no prior expires stamp: fresh expires_at (S1).
	if s := b["expires_at"].(types.String); !s.IsUnknown() {
		t.Errorf("b expires_at = %v, want unknown", s)
	}
	c := elems["c"].(types.Object).Attributes()
	if s := c["visible_at"].(types.String); !s.IsUnknown() {
		t.Errorf("c visible_at = %v, want unknown", s)
	}
	if s := c["expires_at"].(types.String); !s.IsNull() {
		t.Errorf("c expires_at = %v, want null (in inputs)", s)
	}
}

// TestApplyStampsMatchesPlan pins that the apply resolves exactly the
// entries the plan left unknown and keeps every known entry bit for bit.
func TestApplyStampsMatchesPlan(t *testing.T) {
	ctx := context.Background()
	args := stampArgs{
		PlanInputs:       []string{"a", "c"},
		PriorAccumulated: []string{"a", "b"},
		Prior: stamps{
			Expires: map[string]time.Time{},
			Visible: map[string]time.Time{"b": stampBase.Add(time.Hour)},
		},
		PlannedExpires: seconds(7200),
		PriorDelay:     seconds(3600),
		PlannedDelay:   seconds(3600),
	}
	detailed, diags := applyStamps(ctx, []string{"a", "b", "c"}, args, stampBase)
	if diags.HasError() {
		t.Fatalf("applyStamps: %+v", diags)
	}
	got, diags := stampsFromMap(ctx, detailed)
	if diags.HasError() {
		t.Fatalf("stampsFromMap: %+v", diags)
	}
	want := stamps{
		Expires: map[string]time.Time{"b": stampBase.Add(2 * time.Hour)},
		Visible: map[string]time.Time{"b": stampBase.Add(time.Hour), "c": stampBase.Add(time.Hour)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stamps = %+v, want %+v", got, want)
	}
}

// TestApplyFallback pins the unknown-path composition: expiry culls, then
// delay hides, with one clock.
func TestApplyFallback(t *testing.T) {
	ctx := context.Background()
	args := stampArgs{
		PlanInputs:       []string{"a", "d"},
		PriorAccumulated: []string{"a", "b", "c"},
		Prior: stamps{
			Expires: map[string]time.Time{"b": stampBase.Add(-time.Second)},
			Visible: map[string]time.Time{"c": stampBase.Add(time.Hour)},
		},
		PriorExpires:   seconds(7200),
		PlannedExpires: seconds(7200),
		PriorDelay:     seconds(3600),
		PlannedDelay:   seconds(3600),
	}
	accumulated, outputs, detailed, diags := applyFallback(ctx, []string{"a", "b", "c", "d"}, args, stampBase)
	if diags.HasError() {
		t.Fatalf("applyFallback: %+v", diags)
	}
	if want := []string{"a", "c", "d"}; !reflect.DeepEqual(accumulated, want) {
		t.Fatalf("accumulated = %v, want %v (b expired)", accumulated, want)
	}
	if want := []string{"a"}; !reflect.DeepEqual(outputs, want) {
		t.Fatalf("outputs = %v, want %v (c pending, d entering)", outputs, want)
	}
	if got := len(detailed.Elements()); got != 3 {
		t.Fatalf("detailed has %d entries, want 3", got)
	}
}

func TestCheckDelayShorterThanExpiry(t *testing.T) {
	cases := map[string]struct {
		delay, expires types.Int64
		wantErr        bool
	}{
		"both null":       {types.Int64Null(), types.Int64Null(), false},
		"delay only":      {types.Int64Value(10), types.Int64Null(), false},
		"expires only":    {types.Int64Null(), types.Int64Value(10), false},
		"delay shorter":   {types.Int64Value(9), types.Int64Value(10), false},
		"delay equal":     {types.Int64Value(10), types.Int64Value(10), true},
		"delay longer":    {types.Int64Value(11), types.Int64Value(10), true},
		"delay unknown":   {types.Int64Unknown(), types.Int64Value(10), false},
		"expires unknown": {types.Int64Value(10), types.Int64Unknown(), false},
		"both zero":       {types.Int64Value(0), types.Int64Value(0), true},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			diags := checkDelayShorterThanExpiry(tc.delay, tc.expires)
			if diags.HasError() != tc.wantErr {
				t.Fatalf("diags = %+v, wantErr %v", diags, tc.wantErr)
			}
		})
	}
}
