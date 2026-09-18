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
