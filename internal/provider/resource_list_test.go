// SPDX-License-Identifier: MPL-2.0

package provider_test

// The expiry and delay behavior tests that hold for every resource live in
// shared_test.go (TestAccShared); this file keeps only what is specific to the list.

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// listConfig renders an accumulator_list. extra is a raw attribute line or two,
// used for the trigger attributes; pass "" when there are none.
func listConfig(inputs string, length int, extra string) string {
	return fmt.Sprintf(`
resource "accumulator_list" "test" {
  inputs = %s
  length = %d
%s
}
`, inputs, length, extra)
}

func expectListOutputs(outputs ...string) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("accumulator_list.test",
		tfjsonpath.New("outputs"), knownvalue.ListExact(stringList(outputs...)))
}

// expectListPlanOutputs checks the planned (before apply) outputs value is
// known and equals the given elements: the plan must show the list the apply
// will produce, not "(known after apply)".
func expectListPlanOutputs(outputs ...string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue("accumulator_list.test",
		tfjsonpath.New("outputs"), knownvalue.ListExact(stringList(outputs...)))
}

// expectListPlanID checks the planned (before apply) id is known and equals the
// given hash.
func expectListPlanID(id string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue("accumulator_list.test",
		tfjsonpath.New("id"), knownvalue.StringExact(id))
}

func TestAccListCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(`["a"]`, 1, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs("a"),
				// id is HashID of the inputs at creation; sha256(`["a"]`).
				statecheck.ExpectKnownValue("accumulator_list.test", tfjsonpath.New("id"),
					knownvalue.StringExact("0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002")),
			},
			ConfigPlanChecks: expectEmptyAfterRefresh(),
		}},
	})
}

// TestAccListTrimsOnCreate covers "the last item(s) always win" when the initial
// list is longer than length.
func TestAccListTrimsOnCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:            listConfig(`["a", "b"]`, 1, ""),
			ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b")},
			ConfigPlanChecks:  expectEmptyAfterRefresh(),
		}},
	})
}

// TestAccListAppendAndTrim is spec section 6.1's worked example: create ["a"]
// length 2, then ["b"] giving ["a","b"], then ["c"] giving ["b","c"].
func TestAccListAppendAndTrim(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["b"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b"),
					statecheck.ExpectKnownValue("accumulator_list.test", tfjsonpath.New("id"),
						knownvalue.StringExact("0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002")),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["c"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("b", "c"),
					statecheck.ExpectKnownValue("accumulator_list.test", tfjsonpath.New("id"),
						knownvalue.StringExact("0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002")),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListEmptyInputsRetainsHistory is spec section 4 invariant 2: inputs =
// [] never erases history.
func TestAccListEmptyInputsRetainsHistory(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 1, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`[]`, 1, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListLengthShrinkAndGrow covers a length change with unchanged inputs:
// shrinking discards the oldest entries, and growing does not resurrect them.
func TestAccListLengthShrinkAndGrow(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b", "c"]`, 3, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["a", "b", "c"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["a", "b", "c"]`, 3, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListLengthZero empties outputs and keeps them empty.
func TestAccListLengthZero(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 1, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`[]`, 0, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs()},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListUnchangedInputs is spec section 4 invariant 1: a second plan after
// a successful apply is empty.
func TestAccListUnchangedInputs(t *testing.T) {
	config := listConfig(`["a", "b"]`, 2, "")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            config,
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:           config,
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListOutputsKnownAtPlanTimeOnCreate pins that the create plan shows
// the outputs and id the apply will produce, so downstream resources can plan
// against them without waiting for the apply.
func TestAccListOutputsKnownAtPlanTimeOnCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(`["a", "b"]`, 2, ""),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectListPlanOutputs("a", "b"),
					// sha256(`["a","b"]`)
					expectListPlanID("0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
		}},
	})
}

// TestAccListOutputsKnownAtPlanTimeOnAppend pins that an inputs change plans
// the appended outputs, computed against the prior state's history.
func TestAccListOutputsKnownAtPlanTimeOnAppend(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["b"]`, 2, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectListPlanOutputs("a", "b")},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
		},
	})
}

// TestAccListOutputsKnownAtPlanTimeOnLengthChange pins that a length-only
// change plans the re-trimmed outputs.
func TestAccListOutputsKnownAtPlanTimeOnLengthChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b", "c"]`, 3, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["a", "b", "c"]`, 2, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectListPlanOutputs("b", "c")},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b", "c")},
			},
		},
	})
}

// TestAccListOutputsKnownAtPlanTimeOnReset pins that a triggers_reset change
// plans the reseeded outputs.
func TestAccListOutputsKnownAtPlanTimeOnReset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["b"]`, 2, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// The reset discards ["a","b"] and reseeds from ["b"].
				Config: listConfig(`["b"]`, 2, `  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectListPlanOutputs("b")},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b")},
			},
		},
	})
}

// TestAccListOutputsKnownAtPlanTimeOnReplacement pins that a replacement plans
// the reseeded outputs and the new id, both of which Create will produce.
func TestAccListOutputsKnownAtPlanTimeOnReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, `  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["c"]`, 2, `  triggers_replacement = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_list.test", plancheck.ResourceActionReplace),
						expectListPlanOutputs("c"),
						// sha256(`["c"]`), the replacement's id, not the
						// prior resource's id.
						expectListPlanID("fd2079a3096d5abb9bdf54cc5c80262e7b58bac10d524846455474beff760be9"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("c")},
			},
		},
	})
}

// TestAccListOutputsUnknownWhenInputsUnknown pins the unknowable case: when
// the planned inputs are unknown, outputs and id must plan as unknown. The
// branch decision itself is unknowable: the apply produces either the
// reseeded or the appended list, and no planned value can express that.
// uuid() is never known before apply, and the resource is expected to
// diff forever after, because each plan re-evaluates it.
func TestAccListOutputsUnknownWhenInputsUnknown(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:             listConfig(`[uuid()]`, 1, ""),
			ExpectNonEmptyPlan: true,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue("accumulator_list.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_list.test", tfjsonpath.New("id")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("accumulator_list.test",
					tfjsonpath.New("outputs"), knownvalue.ListSizeExact(1)),
			},
		}},
	})
}

// TestAccListFeedsAnotherListAtPlanTime pins the cross-resource effect: a
// downstream accumulator sees the upstream outputs as known values in its own
// plan, on the first plan, before anything has been applied.
func TestAccListFeedsAnotherListAtPlanTime(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = ["x"]
  length = 1
}

resource "accumulator_list" "test" {
  inputs = accumulator_list.src.outputs
  length = 1
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectListPlanOutputs("x"),
					// sha256(`["x"]`), the downstream resource's own id.
					expectListPlanID("cd65ea2c2ad99e94a85b1b6df72efef9cb2ed0ae933a60c32ce16317f7d7d6aa"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("x")},
		}},
	})
}

// TestAccListReset covers triggers_reset discarding history even when inputs did
// not change.
func TestAccListReset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["b"]`, 2, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// triggers_reset changes while inputs stay ["b"]: history is
				// discarded and outputs reseeds from inputs.
				Config:            listConfig(`["b"]`, 2, `  triggers_reset = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListReplacement covers triggers_replacement: a change forces a replace
// that reseeds outputs from inputs, discarding history.
func TestAccListReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, `  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// If this were an in-place update the output would be ["a","c"];
				// a replacement reseeds from ["c"].
				Config:            listConfig(`["c"]`, 2, `  triggers_replacement = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("c")},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_list.test", plancheck.ResourceActionReplace),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// TestAccListLeaverStampedOnInputsChange pins S1 for the list: a and b leave
// inputs when c arrives and are stamped at apply; c stays null, and the
// accumulated order is preserved. The leavers' entries are fresh, so they are
// unknown in the plan and only their apply-time stamps make them concrete.
// The 3600s window keeps the post-refresh plan empty without racing the clock.
func TestAccListLeaverStampedOnInputsChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["c"]`, 10, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "b", "c"),
						// a and b's expires_at values are unknown until the
						// apply stamps them; c is in inputs, so its entry is
						// known and null.
						expectPlanDetailedPartial("accumulator_list.test", oneDetail("c", nullDetail())),
						expectPlanDetailUnknown("accumulator_list.test", "a"),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "c"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": stampedDetail(), "b": stampedDetail(), "c": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListDuplicatesExpireTogether pins the duplicate rule: a appears
// twice, both occurrences share one detailed_outputs entry, and both are
// removed together once a's stamp passes. Step 2's inputs change appends b
// again (list semantics), and leaves a's 1s stamp pending, so it carries no
// post-refresh check; step 3 sleeps past the stamp, and its refresh culls
// both occurrences of a before the plan.
func TestAccListDuplicatesExpireTogether(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "a", "b")},
			},
			{
				Config: listConfig(`["b"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					// [a, a, b] ++ [b]: the list has no deduplication.
					expectListOutputs("a", "a", "b", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": stampedDetail(), "b": nullDetail(),
					}),
				},
			},
			{
				Config:    listConfig(`["b"]`, 10, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls both occurrences of a before the
					// plan, so the plan proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("b", "b"),
					expectDetailed("accumulator_list.test", oneDetail("b", nullDetail())),
				},
			},
		},
	})
}

func expectListAccumulated(values ...string) statecheck.StateCheck {
	return expectAccumulated("accumulator_list.test", knownvalue.ListExact(stringList(values...)))
}

func expectListPlanAccumulated(values ...string) plancheck.PlanCheck {
	return expectPlanAccumulated("accumulator_list.test", knownvalue.ListExact(stringList(values...)))
}

// TestAccListCreateWithDelayedFor pins D1 on create and that length trims
// accumulated, not outputs: [a, b, c] with length 2 keeps [b, c] pending.
func TestAccListCreateWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(`["a", "b", "c"]`, 2, `  delayed_for = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectListPlanOutputs(),
					expectListPlanAccumulated("b", "c"),
					expectPlanDetailVisibleUnknown("accumulator_list.test", "b"),
					expectPlanDetailVisibleUnknown("accumulator_list.test", "c"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs(),
				expectListAccumulated("b", "c"),
				expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
					"b": pendingDetail(), "c": pendingDetail(),
				}),
			},
		}},
	})
}

// TestAccListOutputsKnownAtPlanTimeWithDelayedFor extends the PR #2 suite:
// append, length change, reset, and replacement all plan outputs and
// accumulated as known while delayed_for is set.
func TestAccListOutputsKnownAtPlanTimeWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 10, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
			{
				// Append: a stays visible (D5), b enters and hides (D1).
				Config: listConfig(`["b"]`, 10, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a"),
						expectListPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a"),
					expectListAccumulated("a", "b"),
				},
			},
			{
				// Length-only change re-trims accumulated; b stays pending.
				Config: listConfig(`["b"]`, 1, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs(),
						expectListPlanAccumulated("b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs(), expectListAccumulated("b")},
			},
			{
				// Reset: everything reseeds and enters (T4).
				Config: listConfig(`["b"]`, 1, `  delayed_for = 3600
  triggers_reset = "v1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs(),
						expectListPlanAccumulated("b"),
						expectPlanDetailVisibleUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs(), expectListAccumulated("b")},
			},
			{
				// Replacement: Create reseeds and enters (T4).
				Config: listConfig(`["b"]`, 1, `  delayed_for = 3600
  triggers_reset = "v1"
  triggers_replacement = "r1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_list.test", plancheck.ResourceActionReplace),
						expectListPlanOutputs(),
						expectListPlanAccumulated("b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs(), expectListAccumulated("b")},
			},
		},
	})
}

// TestAccListRevealRealizedOnRefresh pins V1 with duplicates: both
// occurrences of a reveal together at the refresh, and the plan is empty.
func TestAccListRevealRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: listConfig(`["a", "a"]`, 10, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs(),
					expectListAccumulated("a", "a"),
					expectDetailed("accumulator_list.test", oneDetail("a", pendingDetail())),
				},
			},
			{
				Config:    listConfig(`["a", "a"]`, 10, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "a"),
					expectDetailed("accumulator_list.test", oneDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListRevealKeepsHistoryOrder pins the order rule of spec section 7:
// a revealed value lands in its accumulated position, not at the end, and a
// re-appended visible value shares the visible entry.
func TestAccListRevealKeepsHistoryOrder(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 10, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
			{
				// b enters pending: accumulated [a, b], outputs [a].
				Config: listConfig(`["b"]`, 10, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a"),
					expectListAccumulated("a", "b"),
				},
			},
			{
				// a re-appends while visible: accumulated [a, b, a], outputs
				// [a, a]. b is still pending only if the plan runs within 1s,
				// so this step asserts accumulated and lets the reveal land
				// whenever the clock says.
				Config: listConfig(`["a"]`, 10, `  delayed_for = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{expectListPlanAccumulated("a", "b", "a")},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListAccumulated("a", "b", "a")},
			},
			{
				Config:    listConfig(`["a"]`, 10, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "a"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}
