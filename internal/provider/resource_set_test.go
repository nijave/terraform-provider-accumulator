// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// setConfig renders an accumulator_set. extra is a raw attribute line or two,
// used for the trigger attributes; pass "" when there are none.
func setConfig(inputs string, extra string) string {
	return fmt.Sprintf(`
resource "accumulator_set" "test" {
  inputs = %s
%s
}
`, inputs, extra)
}

func expectSetOutputs(outputs ...string) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("accumulator_set.test",
		tfjsonpath.New("outputs"), knownvalue.SetExact(stringList(outputs...)))
}

// expectSetPlanOutputs checks the planned (before apply) outputs value is
// known and equals the given elements: the plan must show the set the apply
// will produce, not "(known after apply)".
func expectSetPlanOutputs(outputs ...string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue("accumulator_set.test",
		tfjsonpath.New("outputs"), knownvalue.SetExact(stringList(outputs...)))
}

// expectSetPlanID checks the planned (before apply) id is known and equals the
// given hash.
func expectSetPlanID(id string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue("accumulator_set.test",
		tfjsonpath.New("id"), knownvalue.StringExact(id))
}

// TestAccSetOutputsKnownAtPlanTimeOnCreate pins that the create plan shows the
// outputs and id the apply will produce.
func TestAccSetOutputsKnownAtPlanTimeOnCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: setConfig(`["a", "b"]`, ""),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectSetPlanOutputs("a", "b"),
					// sha256(`["a","b"]`)
					expectSetPlanID("0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
		}},
	})
}

// TestAccSetOutputsKnownAtPlanTimeOnUnion pins that an inputs change plans the
// unioned outputs.
func TestAccSetOutputsKnownAtPlanTimeOnUnion(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: setConfig(`["b"]`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectSetPlanOutputs("a", "b")},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
		},
	})
}

// TestAccSetOutputsKnownAtPlanTimeOnReset pins that a triggers_reset change
// plans the reseeded outputs.
func TestAccSetOutputsKnownAtPlanTimeOnReset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// The reset discards "a" and reseeds from ["b"].
				Config: setConfig(`["b"]`, `  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectSetPlanOutputs("b")},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("b")},
			},
		},
	})
}

// TestAccSetOutputsUnknownWhenInputsUnknown pins the unknowable case: when
// the planned inputs are unknown, outputs and id must plan as unknown, because
// the branch decision itself is unknowable. uuid() is never known before
// apply, and the resource is expected to diff forever after, because each
// plan re-evaluates it.
func TestAccSetOutputsUnknownWhenInputsUnknown(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:             setConfig(`[uuid()]`, ""),
			ExpectNonEmptyPlan: true,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("id")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("accumulator_set.test",
					tfjsonpath.New("outputs"), knownvalue.SetSizeExact(1)),
			},
		}},
	})
}

// TestAccSetFeedsFromListAtPlanTime pins the cross-resource effect: a set fed
// by an upstream accumulator's outputs sees them as known values in its own
// plan, on the first plan, before anything has been applied.
func TestAccSetFeedsFromListAtPlanTime(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = ["x"]
  length = 1
}

resource "accumulator_set" "test" {
  inputs = accumulator_list.src.outputs
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectSetPlanOutputs("x"),
					// sha256(`["x"]`), the downstream resource's own id.
					expectSetPlanID("cd65ea2c2ad99e94a85b1b6df72efef9cb2ed0ae933a60c32ce16317f7d7d6aa"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("x")},
		}},
	})
}

// TestAccSetUnion is spec section 4 invariant 4: successive updates accumulate,
// each value appearing once, and nothing is forgotten.
func TestAccSetUnion(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            setConfig(`["b"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// Re-submitting values that are already in the set changes
				// nothing, and the set never contains a duplicate.
				Config:            setConfig(`["a", "b"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetDeduplicatesWithinOneApply covers duplicates inside a single
// inputs list.
func TestAccSetDeduplicatesWithinOneApply(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:            setConfig(`["a", "a", "b"]`, ""),
			ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			ConfigPlanChecks:  expectEmptyAfterRefresh(),
		}},
	})
}

// TestAccSetReset covers triggers_reset discarding history and reseeding from
// inputs, which here forgets "a" entirely.
func TestAccSetReset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            setConfig(`["b"]`, `  triggers_reset = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetReplacement covers triggers_replacement reseeding from inputs
// instead of unioning.
func TestAccSetReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// An in-place union would give {a, c}; a replacement gives {c}.
				Config:            setConfig(`["c"]`, `  triggers_replacement = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("c")},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_set.test", plancheck.ResourceActionReplace),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// TestAccSetCreateIdentifiesFromInputs pins the creation identifier.
func TestAccSetCreateIdentifiesFromInputs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: setConfig(`["a"]`, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs("a"),
				// sha256(`["a"]`)
				statecheck.ExpectKnownValue("accumulator_set.test", tfjsonpath.New("id"),
					knownvalue.StringExact("0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002")),
			},
			ConfigPlanChecks: expectEmptyAfterRefresh(),
		}},
	})
}

func expectSetAccumulated(values ...string) statecheck.StateCheck {
	return expectAccumulated("accumulator_set.test", knownvalue.SetExact(stringList(values...)))
}

func expectSetPlanAccumulated(values ...string) plancheck.PlanCheck {
	return expectPlanAccumulated("accumulator_set.test", knownvalue.SetExact(stringList(values...)))
}

// TestAccSetCreateWithDelayedFor pins D1 on create: outputs plans as known
// and empty, accumulated as known and seeded, and every visible_at is
// unknown until the apply stamps it. The 3600s window keeps the
// post-refresh plan empty.
func TestAccSetCreateWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: setConfig(`["a", "b"]`, `  delayed_for = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectSetPlanOutputs(),
					expectSetPlanAccumulated("a", "b"),
					expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					expectPlanDetailVisibleUnknown("accumulator_set.test", "b"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs(),
				expectSetAccumulated("a", "b"),
				expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
					"a": pendingDetail(), "b": pendingDetail(),
				}),
			},
		}},
	})
}

// TestAccSetOutputsKnownAtPlanTimeWithDelayedFor extends the PR #2 suite:
// union, reset, and replacement all plan outputs and accumulated as known
// while delayed_for is set.
func TestAccSetOutputsKnownAtPlanTimeWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
			{
				// Union: a stays visible (D5), b enters and hides (D1).
				Config: setConfig(`["a", "b"]`, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a"),
						expectSetPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectSetAccumulated("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": pendingDetail(),
					}),
				},
			},
			{
				// Reset: everything reseeds and enters (T4).
				Config: setConfig(`["a", "b"]`, `  delayed_for = 3600
  triggers_reset = "v1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs(), expectSetAccumulated("a", "b")},
			},
			{
				// Replacement: Create reseeds and enters (T4).
				Config: setConfig(`["a", "b"]`, `  delayed_for = 3600
  triggers_reset = "v1"
  triggers_replacement = "r1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_set.test", plancheck.ResourceActionReplace),
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs(), expectSetAccumulated("a", "b")},
			},
		},
	})
}

// TestAccSetRevealRealizedOnRefresh pins V1: with no configuration change,
// the step's refresh reveals a before planning, so the plan is empty and
// outputs now holds it with a null visible_at.
func TestAccSetRevealRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a"]`, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", oneDetail("a", pendingDetail())),
				},
			},
			{
				Config:    setConfig(`["a"]`, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectSetAccumulated("a"),
					expectDetailed("accumulator_set.test", oneDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetDelayedForRemoved pins T2: removing delayed_for reveals every
// pending value at that apply, and the plan shows the revealed outputs.
func TestAccSetDelayedForRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a", "b"]`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
		},
	})
}

// TestAccSetDelayedForDecreaseRevealsSooner pins D3 end to end: a was
// stamped +1h; decreasing to 1 recalculates min(old, now+1s) at apply, and
// the following refresh reveals it. Step 2 leaves a 1s stamp pending, so it
// carries no post-refresh check.
func TestAccSetDelayedForDecreaseRevealsSooner(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectDetailed("accumulator_set.test", oneDetail("a", pendingDetail())),
				},
			},
			{
				Config:    setConfig(`["a"]`, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
		},
	})
}

// TestAccSetDelayedForIncreaseKeepsPending pins D4: an increase restamps
// the pending value (fresh, unknown in the plan) and it stays hidden.
func TestAccSetDelayedForIncreaseKeepsPending(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 7200`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", oneDetail("a", pendingDetail())),
				},
			},
		},
	})
}

// TestAccSetDelayedForZeroRevealsNextRefresh pins the delayed_for = 0
// reading of V1: the apply stamps the current moment and leaves outputs
// unchanged; the next refresh reveals.
func TestAccSetDelayedForZeroRevealsNextRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a"]`, `  delayed_for = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{expectSetPlanOutputs()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// The apply stamps a with the current moment; the refresh
					// that reveals it runs after these checks.
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", oneDetail("a", pendingDetail())),
				},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", oneDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetDelayedForMustBeShorterThanExpiresAfter pins C1: equal and
// greater are rejected before any plan; shorter is accepted.
func TestAccSetDelayedForMustBeShorterThanExpiresAfter(t *testing.T) {
	for _, tc := range []struct{ name, extra string }{
		{"equal", "  delayed_for = 10\n  expires_after = 10"},
		{"greater", "  delayed_for = 11\n  expires_after = 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      setConfig(`["a"]`, tc.extra),
					ExpectError: regexp.MustCompile(`delayed_for must be shorter than expires_after`),
				}},
			})
		})
	}
	t.Run("shorter", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{{
				Config:            setConfig(`["a"]`, "  delayed_for = 9\n  expires_after = 10"),
				ConfigStateChecks: []statecheck.StateCheck{expectSetAccumulated("a")},
			}},
		})
	})
}

// TestAccSetPendingLeaverStamped pins T5: a value that leaves inputs while
// pending keeps visible_at and gains expires_at.
func TestAccSetPendingLeaverStamped(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a"]`, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a", "b"),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": pendingDetail(), "b": pendingLeaverDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetResetDelaysSeed pins T4 with a value that was already visible:
// a reset reseeds and the seeded value enters again with a full window.
func TestAccSetResetDelaysSeed(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 1`),
				PreConfig:         sleepPastExpiry(),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 3600
  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a"),
						expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", oneDetail("a", pendingDetail())),
				},
			},
		},
	})
}

// TestAccSetUnknownDelayedForAtPlan pins the fallback path: an unknown
// delayed_for leaves outputs, accumulated, and detailed_outputs unknown at
// plan, and the apply resolves them with its own clock.
func TestAccSetUnknownDelayedForAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_set" "test" {
  inputs      = ["a"]
  delayed_for = length(accumulator_list.src.outputs) * 3600
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:             config,
			ExpectNonEmptyPlan: true,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("accumulated")),
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs(),
				expectSetAccumulated("a"),
				expectDetailed("accumulator_set.test", oneDetail("a", pendingDetail())),
			},
		}},
	})
}

// TestAccSetImportThenDelayedFor pins spec section 8: imported values seed
// visible, and the first new value in the settling apply is delayed.
func TestAccSetImportThenDelayedFor(t *testing.T) {
	config := `
resource "accumulator_set" "test" {
  inputs      = ["d"]
  delayed_for = 3600
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_set.test",
				ImportState:        true,
				ImportStateId:      `{"outputs":["a","b"]}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						expectSetPlanAccumulated("a", "b", "d"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(), "d": pendingDetail(),
					}),
				},
			},
		},
	})
}
