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
