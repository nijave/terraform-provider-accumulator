// SPDX-License-Identifier: GPL-3.0-or-later

package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// TestAccSetCreateWithExpiresAfter pins the create shape: every value is in
// inputs, so detailed_outputs is fully known at plan time and all null.
func TestAccSetCreateWithExpiresAfter(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: setConfig(`["a", "b"]`, `  expires_after = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectSetPlanOutputs("a", "b"),
					expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs("a", "b"),
				expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
					"a": nullDetail(), "b": nullDetail(),
				}),
			},
		}},
	})
}

// TestAccSetLeaverStampedOnInputsChange pins S1: b leaves inputs and is
// stamped at apply; a stays null. b's entry is fresh, so it is unknown in the
// plan and only its apply-time stamp makes it concrete. The 3600s window keeps
// the post-refresh plan empty without racing the clock.
func TestAccSetLeaverStampedOnInputsChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						// b's expires_at is unknown until the apply stamps it.
						expectPlanDetailedPartial("accumulator_set.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetExpiryRealizedOnRefresh pins X1: with no configuration change,
// the step's refresh culls b before planning, so the plan is empty and state
// no longer contains it. Step 2 deliberately leaves a 1s stamp pending, so it
// carries no post-refresh check; step 3 sleeps past the stamp and re-applies
// the same config.
func TestAccSetExpiryRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config:    setConfig(`["a"]`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan, so the plan
					// proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetInInputsNeverExpires pins R2 and the steady-state invariant: with
// expires_after = 1 held over a sleep, a stays unstamped, unremoved, and the
// plan after the sleep is empty (invariant 1 holds with expires_after set).
func TestAccSetInInputsNeverExpires(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
			{
				Config:    setConfig(`["a"]`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterSetOnExistingResource pins S5: history that
// accumulated with no expires_after gets a full window when it is enabled.
func TestAccSetExpiresAfterSetOnExistingResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a", "b"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// b is fresh history; its expires_at is unknown until
						// the apply stamps it (S5).
						expectPlanDetailedPartial("accumulator_set.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterRemoved pins T2: removing expires_after expires
// nothing and nulls every expires_at.
func TestAccSetExpiresAfterRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// T2 removes nothing: b survives the removal of
						// expires_after, with its stamp nulled.
						expectSetPlanOutputs("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterZeroCullsNextRefresh pins X1's E=0 case: b's entry is
// fresh in the E-zero plan and the apply stamps it with the current moment, so
// the next refresh culls it.
func TestAccSetExpiresAfterZeroCullsNextRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						// b's stamp is fresh (a decrease to 0), unknown until
						// the apply stamps it "now".
						expectPlanDetailedPartial("accumulator_set.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// The apply stamps b with the current moment; the refresh
					// that culls it runs after these checks.
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan.
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterDecreasePullsEarlier pins S3 end to end: b was
// stamped +1h; decreasing to 1 recalculates min(old, ~now+1s) at apply, and the
// following refresh culls it. Step 3 leaves a 1s stamp pending, so it carries
// no post-refresh check.
func TestAccSetExpiresAfterDecreasePullsEarlier(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						// A decrease recalculates b's stamp (min(old,
						// candidate)), so it is fresh and unknown until the
						// apply.
						expectPlanDetailedPartial("accumulator_set.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config:    setConfig(`["a"]`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan, so the plan
					// proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetIncreaseDoesNotResurrect pins the spec's accepted X1 consequence:
// b's 1s window has passed by step 3's refresh, which culls it before the plan
// can apply S4, so raising expires_after cannot bring it back. Step 2 leaves the
// 1s stamp pending, so it carries no post-refresh check.
func TestAccSetIncreaseDoesNotResurrect(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:    setConfig(`["a"]`, `  expires_after = 3600`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// b was culled at refresh; the plan only shows a.
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a"),
						expectPlanDetailed("accumulator_set.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetReSupplyRescues pins T5: b's 1s window has passed unrealized,
// and re-adding b to inputs rescues it with a null stamp. Step 2 leaves the
// 1s stamp pending, so it carries no post-refresh check.
func TestAccSetReSupplyRescues(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:    setConfig(`["a", "b"]`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetResetClearsStamps pins T4: a reset reseeds from inputs alone, so
// detailed_outputs is all null again.
func TestAccSetResetClearsStamps(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a", "b"]`, `  expires_after = 3600
  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600
  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600
  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a"),
						expectPlanDetailed("accumulator_set.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetReplacementClearsStamps pins T4's replacement half: a
// triggers_replacement change destroys and recreates, Create reseeds from
// inputs alone, and detailed_outputs is all null again.
func TestAccSetReplacementClearsStamps(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a", "b"]`, `  expires_after = 3600
  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600
  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600
  triggers_replacement = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_set.test", plancheck.ResourceActionReplace),
						expectSetPlanOutputs("a"),
						expectPlanDetailed("accumulator_set.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetUnknownExpiresAfterAtPlan pins the fallback path: expires_after
// derived from an upstream unknown stays unknown at plan, so outputs and
// detailed_outputs plan as unknown and the apply resolves them with the
// apply clock. The upstream resource diffs forever (uuid), so there is no
// post-refresh check, matching TestAccSetOutputsUnknownWhenInputsUnknown.
func TestAccSetUnknownExpiresAfterAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_set" "test" {
  inputs         = ["a"]
  expires_after  = length(accumulator_list.src.outputs)
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
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs("a"),
				expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
			},
		}},
	})
}

// TestAccSetImportThenExpiresAfter pins spec section 8: the import ID
// carries no expiration data, imported entries are null, and enabling
// expires_after in the settling apply stamps non-inputs values via S5.
func TestAccSetImportThenExpiresAfter(t *testing.T) {
	config := `
resource "accumulator_set" "test" {
  inputs        = ["a"]
  expires_after = 3600
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
				ImportStateCheck:   importSeedsNullDetails("a", "b"),
			},
			{
				// The settling apply: a is in inputs (null), b is seeded
				// history with no prior stamp, so S5 stamps it.
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// b is fresh history; its expires_at is unknown until
						// the apply stamps it.
						expectPlanDetailedPartial("accumulator_set.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// importSeedsNullDetails is the ImportStateCheck shared by the import-then-
// expire tests: import seeds one detailed_outputs entry per seeded output,
// each with a null expires_at (spec section 8). It inspects the raw flatmap
// because terraform-plugin-testing runs state checks only for Config steps,
// not import steps. Null object attributes are omitted from the flatmap, so
// each seeded value must have an entry map of size 1 and no non-empty
// expires_at.
func importSeedsNullDetails(values ...string) func([]*terraform.InstanceState) error {
	return func(states []*terraform.InstanceState) error {
		if len(states) != 1 {
			return fmt.Errorf("imported %d resources, want 1", len(states))
		}
		attrs := states[0].Attributes
		if got := attrs["detailed_outputs.%"]; got != fmt.Sprint(len(values)) {
			return fmt.Errorf("detailed_outputs.%% = %q, want %d", got, len(values))
		}
		for _, value := range values {
			if got := attrs["detailed_outputs."+value+".%"]; got != "1" {
				return fmt.Errorf("detailed_outputs.%s.%% = %q, want 1", value, got)
			}
			path := "detailed_outputs." + value + ".expires_at"
			if got, ok := attrs[path]; ok && got != "" {
				return fmt.Errorf("%s = %q, want null", path, got)
			}
		}
		return nil
	}
}

// TestAccSetExpiresAfterIncreaseKeepsLive pins S4 for the set: an
// expires_after increase re-times a still-live value to max(old, candidate)
// and keeps it in outputs. The 300s first window keeps b alive across the
// test; the 3600s increase pushes its stamp later. b is fresh under the
// change, so it is unknown in each of those plans.
func TestAccSetExpiresAfterIncreaseKeepsLive(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a", "b"]`, `  expires_after = 300`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 300`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						expectPlanDetailedPartial("accumulator_set.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// b survives the increase and is re-stamped, so it is
						// still present and its entry is unknown until the
						// apply.
						expectSetPlanOutputs("a", "b"),
						expectPlanDetailedPartial("accumulator_set.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterUpperBound pins the validation upper bound: a value
// above maxExpiresAfter (9223372036) is rejected at plan time by the
// framework's between-validator.
func TestAccSetExpiresAfterUpperBound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      setConfig(`["a"]`, `  expires_after = 10000000000`),
			ExpectError: regexp.MustCompile(`value must be between`),
		}},
	})
}
