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

// TestAccSetOutputsUnknownWhenInputsUnknown pins the conservative case: when
// the planned inputs are unknown, outputs and id must plan as unknown.
// uuid() is never known before apply, and the resource is expected to diff
// forever after, because each plan re-evaluates it.
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
