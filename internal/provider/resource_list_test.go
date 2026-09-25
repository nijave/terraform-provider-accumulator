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

// TestAccListCreateWithExpiresAfter pins the create shape for the list
// resource: all values are in inputs, so detailed_outputs is fully known at
// plan time and all null.
func TestAccListCreateWithExpiresAfter(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(`["a"]`, 5, `  expires_after = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectListPlanOutputs("a"),
					expectPlanDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs("a"),
				expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
			},
		}},
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
						expectPlanDetailedPartial("accumulator_list.test", setDetail("c", nullDetail())),
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

// TestAccListExpiryRealizedOnRefresh pins X1: with no configuration change,
// the step's refresh culls a before planning, so the plan is empty and state
// no longer contains it. Step 2's inputs change [a] -> [b] appends b (list
// semantics: the whole new list is appended) and leaves a with a pending 1s
// stamp, so it carries no post-refresh check; step 3 sleeps past the stamp
// and re-applies the same config.
func TestAccListExpiryRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
			{
				Config: listConfig(`["b"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": stampedDetail(), "b": nullDetail(),
					}),
				},
			},
			{
				Config:    listConfig(`["b"]`, 10, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls a before the plan, so the plan
					// proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("b"),
					expectDetailed("accumulator_list.test", setDetail("b", nullDetail())),
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
					expectDetailed("accumulator_list.test", setDetail("b", nullDetail())),
				},
			},
		},
	})
}

// TestAccListInInputsNeverExpires pins R2 and the steady-state invariant: with
// expires_after = 1 held over a sleep, a stays unstamped, unremoved, and the
// plan after the sleep is empty (invariant 1 holds with expires_after set).
func TestAccListInInputsNeverExpires(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
			{
				Config:    listConfig(`["a"]`, 10, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a"),
					expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListExpiresAfterSetOnExistingResource pins S5: history that
// accumulated with no expires_after gets a full window when it is enabled.
// The list appends the whole new inputs list, so ["a"] after ["a","b"] keeps
// the history and appends ["a"] again.
func TestAccListExpiresAfterSetOnExistingResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: listConfig(`["a", "b"]`, 10, ""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "b", "a"),
						// b is fresh history; its expires_at is unknown until
						// the apply stamps it (S5). a is in inputs, so null.
						expectPlanDetailedPartial("accumulator_list.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "a"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListExpiresAfterRemoved pins T2: removing expires_after expires
// nothing and nulls every expires_at.
func TestAccListExpiresAfterRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "a")},
			},
			{
				Config: listConfig(`["a"]`, 10, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// T2 removes nothing: b survives the removal of
						// expires_after, with its stamp nulled.
						expectListPlanOutputs("a", "b", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
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

// TestAccListExpiresAfterZeroCullsNextRefresh pins X1's E=0 case: b's entry is
// fresh in the E-zero plan and the apply stamps it with the current moment, so
// the next refresh culls it.
func TestAccListExpiresAfterZeroCullsNextRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "a")},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "b", "a"),
						// b's stamp is fresh (a decrease to 0), unknown until
						// the apply stamps it "now".
						expectPlanDetailedPartial("accumulator_list.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// The apply stamps b with the current moment; the refresh
					// that culls it runs after these checks.
					expectListOutputs("a", "b", "a"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan.
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "a"),
					expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListExpiresAfterDecreasePullsEarlier pins S3 end to end: b was
// stamped +1h; decreasing to 1 recalculates min(old, ~now+1s) at apply, and the
// following refresh culls it. Step 4 leaves a 1s stamp pending, so it carries
// no post-refresh check.
func TestAccListExpiresAfterDecreasePullsEarlier(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "a")},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "b", "a"),
						// A decrease recalculates b's stamp (min(old,
						// candidate)), so it is fresh and unknown until the
						// apply.
						expectPlanDetailedPartial("accumulator_list.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "a"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config:    listConfig(`["a"]`, 10, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan, so the plan
					// proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "a"),
					expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListIncreaseDoesNotResurrect pins the spec's accepted X1
// consequence: b's 1s window has passed by step 3's refresh, which culls it
// before the plan can apply S4, so raising expires_after cannot bring it back.
// Step 2 leaves the 1s stamp pending, so it carries no post-refresh check.
func TestAccListIncreaseDoesNotResurrect(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "a")},
			},
			{
				Config:    listConfig(`["a"]`, 10, `  expires_after = 3600`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// b was culled at refresh; the plan only shows a.
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "a"),
						expectPlanDetailed("accumulator_list.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "a"),
					expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListExpiresAfterIncreaseKeepsLive pins S4 for the list: an
// expires_after increase re-times a still-live value to max(old, candidate)
// and keeps it in outputs. The list appends, so ["a"] after ["a","b"] yields
// ["a","b","a"]. The 300s first window keeps b alive; the 3600s increase
// pushes its stamp later, and b is unknown in each plan that re-stamps it.
func TestAccListExpiresAfterIncreaseKeepsLive(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: listConfig(`["a", "b"]`, 10, `  expires_after = 300`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 300`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "b", "a"),
						expectPlanDetailedPartial("accumulator_list.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "a"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// b survives the increase and is re-stamped, so it is
						// still present and its entry is unknown until the
						// apply.
						expectListPlanOutputs("a", "b", "a"),
						expectPlanDetailedPartial("accumulator_list.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "a"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListReSupplyRescues pins T5: b's 1s window has passed unrealized,
// and re-adding b to inputs rescues it with a null stamp. Because the list
// appends rather than unions, ["a","b"] after the culled ["a","a"] yields
// ["a","a","a","b"]. Step 2 leaves the 1s stamp pending, so it carries no
// post-refresh check.
func TestAccListReSupplyRescues(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "a")},
			},
			{
				Config:    listConfig(`["a", "b"]`, 10, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "a", "a", "b"),
						expectPlanDetailed("accumulator_list.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "a", "a", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListResetClearsStamps pins T4: a reset reseeds from inputs alone, so
// detailed_outputs is all null again.
func TestAccListResetClearsStamps(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: listConfig(`["a", "b"]`, 10, `  expires_after = 3600
  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 3600
  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "a")},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 3600
  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a"),
						expectPlanDetailed("accumulator_list.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a"),
					expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListReplacementClearsStamps pins T4's replacement half: a
// triggers_replacement change destroys and recreates, Create reseeds from
// inputs alone, and detailed_outputs is all null again.
func TestAccListReplacementClearsStamps(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: listConfig(`["a", "b"]`, 10, `  expires_after = 3600
  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 3600
  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "a")},
			},
			{
				Config: listConfig(`["a"]`, 10, `  expires_after = 3600
  triggers_replacement = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_list.test", plancheck.ResourceActionReplace),
						expectListPlanOutputs("a"),
						expectPlanDetailed("accumulator_list.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a"),
					expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListUnknownExpiresAfterAtPlan pins the fallback path: expires_after
// derived from an upstream unknown stays unknown at plan, so outputs and
// detailed_outputs plan as unknown and the apply resolves them with the
// apply clock. The upstream resource diffs forever (uuid), so there is no
// post-refresh check, matching TestAccListOutputsUnknownWhenInputsUnknown.
func TestAccListUnknownExpiresAfterAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_list" "test" {
  inputs        = ["a"]
  length        = 1
  expires_after = length(accumulator_list.src.outputs)
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
					plancheck.ExpectUnknownValue("accumulator_list.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_list.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs("a"),
				expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
			},
		}},
	})
}

// TestAccListImportThenExpiresAfter pins spec section 8 for the list: the
// import ID carries no expiration data, imported entries are null, and
// enabling expires_after in the settling apply stamps non-inputs values via
// S5.
func TestAccListImportThenExpiresAfter(t *testing.T) {
	config := `
resource "accumulator_list" "test" {
  inputs        = ["a"]
  length        = 3
  expires_after = 3600
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_list.test",
				ImportState:        true,
				ImportStateId:      `{"inputs":["a"],"outputs":["a","b"]}`,
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
						expectListPlanOutputs("a", "b"),
						expectPlanDetailedPartial("accumulator_list.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListExpiresAfterUpperBound pins the validation upper bound: a value
// above maxExpiresAfter (9223372036) is rejected at plan time by the
// framework's between-validator.
func TestAccListExpiresAfterUpperBound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      listConfig(`["a"]`, 1, `  expires_after = 10000000000`),
			ExpectError: regexp.MustCompile(`value must be between`),
		}},
	})
}
