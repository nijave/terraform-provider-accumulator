// SPDX-License-Identifier: GPL-3.0-or-later

package provider_test

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
