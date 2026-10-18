// SPDX-License-Identifier: MPL-2.0

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

// mapConfig renders an accumulator_map. inputs is an HCL map literal such
// as `{ a = "1" }`; extra is a raw attribute line or two, "" for none.
func mapConfig(inputs string, extra string) string {
	return fmt.Sprintf(`
resource "accumulator_map" "test" {
  inputs = %s
%s
}
`, inputs, extra)
}

// stringMap widens a Go map into the knownvalue check map MapExact takes.
func stringMap(values map[string]string) map[string]knownvalue.Check {
	checks := make(map[string]knownvalue.Check, len(values))
	for k, v := range values {
		checks[k] = knownvalue.StringExact(v)
	}
	return checks
}

func expectMapOutputs(values map[string]string) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("accumulator_map.test",
		tfjsonpath.New("outputs"), knownvalue.MapExact(stringMap(values)))
}

// expectMapPlanOutputs checks the planned (before apply) outputs is known
// and equals the given entries: the plan must show the map the apply will
// produce, not "(known after apply)".
func expectMapPlanOutputs(values map[string]string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue("accumulator_map.test",
		tfjsonpath.New("outputs"), knownvalue.MapExact(stringMap(values)))
}

func expectMapAccumulated(values map[string]string) statecheck.StateCheck {
	return expectAccumulated("accumulator_map.test", knownvalue.MapExact(stringMap(values)))
}

func expectMapPlanAccumulated(values map[string]string) plancheck.PlanCheck {
	return expectPlanAccumulated("accumulator_map.test", knownvalue.MapExact(stringMap(values)))
}

func expectMapPlanID(id string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue("accumulator_map.test",
		tfjsonpath.New("id"), knownvalue.StringExact(id))
}

// TestAccMapOutputsKnownAtPlanTimeOnCreate pins that the create plan shows
// the outputs and id the apply will produce, and that every entry starts
// with null stamps.
func TestAccMapOutputsKnownAtPlanTimeOnCreate(t *testing.T) {
	want := map[string]string{"a": "1", "b": "1"}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: mapConfig(`{ a = "1", b = "1" }`, ""),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectMapPlanOutputs(want),
					expectMapPlanAccumulated(want),
					// sha256(`{"a":"1","b":"1"}`)
					expectMapPlanID("40239544dc03c3ec3a09cb947c72123697428a72af96b1c63431f34a46517709"),
					expectPlanDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectMapOutputs(want),
				expectMapAccumulated(want),
				expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
					"a": nullDetail(), "b": nullDetail(),
				}),
			},
		}},
	})
}

// TestAccMapEmptyInputs pins the degenerate create: inputs = {} yields
// empty (not null) maps and the hash of "{}".
func TestAccMapEmptyInputs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: mapConfig(`{}`, ""),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectMapPlanOutputs(map[string]string{}),
					// sha256(`{}`)
					expectMapPlanID("44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectMapOutputs(map[string]string{}),
				expectMapAccumulated(map[string]string{}),
				expectDetailed("accumulator_map.test", map[string]knownvalue.Check{}),
			},
		}},
	})
}

// TestAccMapMerge pins M1: successive applies accumulate keys, prior keys
// keep their values, and re-supplying a key with the same value changes
// nothing.
func TestAccMapMerge(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: mapConfig(`{ b = "2" }`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectMapPlanOutputs(map[string]string{"a": "1", "b": "2"})},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "2"})},
			},
			{
				// Same values re-supplied. inputs round-trips through state
				// ({ b = "2" } before), so the plan is an update, but the
				// outputs it plans are unchanged.
				Config: mapConfig(`{ a = "1", b = "2" }`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_map.test", plancheck.ResourceActionUpdate),
						expectMapPlanOutputs(map[string]string{"a": "1", "b": "2"}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "2"})},
			},
		},
	})
}

// TestAccMapOverwriteVisibleKey pins V1: a visible key re-supplied with a
// new value shows the new value in accumulated and outputs at that apply,
// with the plan fully known and the entry's stamps still null.
func TestAccMapOverwriteVisibleKey(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: mapConfig(`{ a = "2" }`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "2", "b": "1"}),
						expectMapPlanAccumulated(map[string]string{"a": "2", "b": "1"}),
						expectPlanDetailed("accumulator_map.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "2", "b": "1"}),
					expectMapAccumulated(map[string]string{"a": "2", "b": "1"}),
				},
			},
			{
				// An empty string is a real value and overwrites like any other.
				Config: mapConfig(`{ a = "" }`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectMapPlanOutputs(map[string]string{"a": "", "b": "1"})},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "", "b": "1"})},
			},
		},
	})
}

// TestAccMapKeysWithSeparators pins that keys containing "." and "/" are
// opaque strings: they round-trip through outputs and detailed_outputs
// without being split into nested paths.
func TestAccMapKeysWithSeparators(t *testing.T) {
	want := map[string]string{"svc/web": "1", "a.b": "2"}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: mapConfig(`{ "svc/web" = "1", "a.b" = "2" }`, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				expectMapOutputs(want),
				expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
					"svc/web": nullDetail(), "a.b": nullDetail(),
				}),
			},
			ConfigPlanChecks: expectEmptyAfterRefresh(),
		}},
	})
}

// TestAccMapReset covers triggers_reset discarding history and reseeding
// from inputs, which here forgets "a" entirely and keeps the id.
func TestAccMapReset(t *testing.T) {
	// sha256(`{"a":"1"}`)
	const id = "9afeb0f2b203f254312ec8ded441d0318b7c34c57f8695ede42d2215a30c0960"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: mapConfig(`{ b = "2" }`, `  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"b": "2"}),
						expectMapPlanID(id),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"b": "2"})},
			},
		},
	})
}

// TestAccMapReplacement covers triggers_replacement reseeding from inputs
// instead of merging, with a recomputed id.
func TestAccMapReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// An in-place merge would give {a, x}; a replacement gives {x}.
				Config: mapConfig(`{ x = "1" }`, `  triggers_replacement = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_map.test", plancheck.ResourceActionReplace),
						expectMapPlanOutputs(map[string]string{"x": "1"}),
						// sha256(`{"x":"1"}`)
						expectMapPlanID("013bdcb2b4d38b7d26f13f2a48631e9a5ae45530c3ef0e5946668d4f1892baa1"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"x": "1"})},
			},
		},
	})
}

// TestAccMapOutputsUnknownWhenInputsUnknown pins the unknowable case: an
// unknown value inside inputs leaves outputs and id unknown at plan, and
// the apply resolves them. uuid() is never known before apply, so the
// resource diffs forever after.
func TestAccMapOutputsUnknownWhenInputsUnknown(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:             mapConfig(`{ a = uuid() }`, ""),
			ExpectNonEmptyPlan: true,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("accumulated")),
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("id")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("accumulator_map.test",
					tfjsonpath.New("outputs"), knownvalue.MapSizeExact(1)),
				expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
			},
		}},
	})
}

// TestAccMapFeedsFromSetAtPlanTime pins the cross-resource effect: a map
// built from an upstream accumulator's outputs plans as known on the first
// plan.
func TestAccMapFeedsFromSetAtPlanTime(t *testing.T) {
	config := `
resource "accumulator_set" "src" {
  inputs = ["x"]
}

resource "accumulator_map" "test" {
  inputs = { for v in accumulator_set.src.outputs : v => "1" }
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectMapPlanOutputs(map[string]string{"x": "1"}),
					// sha256(`{"x":"1"}`)
					expectMapPlanID("013bdcb2b4d38b7d26f13f2a48631e9a5ae45530c3ef0e5946668d4f1892baa1"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"x": "1"})},
		}},
	})
}
