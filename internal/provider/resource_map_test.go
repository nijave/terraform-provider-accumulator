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

// TestAccMapLeaverStampedOnInputsChange pins S1 for keys: b leaves inputs
// and is stamped at apply; a stays null and keeps its value. b's entry is
// fresh, so it is unknown in the plan.
func TestAccMapLeaverStampedOnInputsChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1", "b": "1"}),
						expectPlanDetailUnknown("accumulator_map.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapValueChangeDoesNotStamp pins Review Focus 4: with expires_after
// set, a value-only change plans fully known with no unknown stamp, because
// no key enters or leaves.
func TestAccMapValueChangeDoesNotStamp(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: mapConfig(`{ a = "2" }`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "2"}),
						expectPlanDetailed("accumulator_map.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "2"}),
					expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccMapExpiryRealizedOnRefresh pins X1: with no configuration change,
// the step's refresh culls b before planning, so the plan is empty and
// state no longer contains it.
func TestAccMapExpiryRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config:    mapConfig(`{ a = "1" }`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectMapAccumulated(map[string]string{"a": "1"}),
					expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccMapReSupplyRescues pins that a stamped leaver re-supplied with a
// new value loses its expires_at and takes the new value.
func TestAccMapReSupplyRescues(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: mapConfig(`{ a = "1", b = "9" }`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1", "b": "9"}),
						expectPlanDetailed("accumulator_map.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "9"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapResetClearsStamps pins T4 for expiry: a reset forgets stamped
// leavers along with everything else.
func TestAccMapResetClearsStamps(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, "  expires_after = 3600\n  triggers_reset = \"v1\""),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, "  expires_after = 3600\n  triggers_reset = \"v1\""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: mapConfig(`{ a = "1" }`, "  expires_after = 3600\n  triggers_reset = \"v2\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1"}),
						expectPlanDetailed("accumulator_map.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccMapExpiresAfterSetOnExistingResource pins S5: history that
// accumulated with no expires_after gets a full window when it is enabled.
func TestAccMapExpiresAfterSetOnExistingResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mapConfig(`{ a = "1", b = "1" }`, ""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// b is fresh history; its expires_at is unknown until
						// the apply stamps it (S5).
						expectPlanDetailedPartial("accumulator_map.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_map.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapExpiresAfterRemoved pins T2: removing expires_after expires
// nothing and nulls every expires_at.
func TestAccMapExpiresAfterRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
			},
			{
				Config:            mapConfig(`{ a = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// T2 removes nothing: b survives the removal of
						// expires_after, with its stamp nulled.
						expectMapPlanOutputs(map[string]string{"a": "1", "b": "1"}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapExpiresAfterDecreasePullsEarlier pins S3 end to end: b was
// stamped +1h; decreasing to 1 recalculates min(old, ~now+1s) at apply, and the
// following refresh culls it. Step 3 leaves a 1s stamp pending, so it carries
// no post-refresh check.
func TestAccMapExpiresAfterDecreasePullsEarlier(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
			},
			{
				Config:            mapConfig(`{ a = "1" }`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "b": "1"})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  expires_after = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1", "b": "1"}),
						// A decrease recalculates b's stamp (min(old,
						// candidate)), so it is fresh and unknown until the
						// apply.
						expectPlanDetailedPartial("accumulator_map.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_map.test", "b"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config:    mapConfig(`{ a = "1" }`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan, so the plan
					// proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccMapExpiresAfterIncreaseKeepsLive pins S4 for the map: an
// expires_after increase re-times a still-live key to max(old, candidate)
// and keeps it in outputs. The 300s first window keeps b alive across the
// test; the 3600s increase pushes its stamp later. b is fresh under the
// change, so it is unknown in each of those plans.
func TestAccMapExpiresAfterIncreaseKeepsLive(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mapConfig(`{ a = "1", b = "1" }`, `  expires_after = 300`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  expires_after = 300`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1", "b": "1"}),
						expectPlanDetailedPartial("accumulator_map.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_map.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// b survives the increase and is re-stamped, so it is
						// still present and its entry is unknown until the
						// apply.
						expectMapPlanOutputs(map[string]string{"a": "1", "b": "1"}),
						expectPlanDetailedPartial("accumulator_map.test", setDetail("a", nullDetail())),
						expectPlanDetailUnknown("accumulator_map.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapUnknownExpiresAfterAtPlan pins the fallback path: expires_after
// derived from an upstream unknown stays unknown at plan, so outputs and
// detailed_outputs plan as unknown and the apply resolves them with the
// apply clock. The upstream resource diffs forever (uuid), so there is no
// post-refresh check, matching TestAccMapOutputsUnknownWhenInputsUnknown.
func TestAccMapUnknownExpiresAfterAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_map" "test" {
  inputs         = { a = "1" }
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
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectMapOutputs(map[string]string{"a": "1"}),
				expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
			},
		}},
	})
}

// TestAccMapCreateWithDelayedFor pins D1 on create: outputs plans as known
// and empty, accumulated as known and seeded, and every visible_at is
// unknown until the apply stamps it.
func TestAccMapCreateWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: mapConfig(`{ a = "1", b = "1" }`, `  delayed_for = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectMapPlanOutputs(map[string]string{}),
					expectMapPlanAccumulated(map[string]string{"a": "1", "b": "1"}),
					expectPlanDetailVisibleUnknown("accumulator_map.test", "a"),
					expectPlanDetailVisibleUnknown("accumulator_map.test", "b"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectMapOutputs(map[string]string{}),
				expectMapAccumulated(map[string]string{"a": "1", "b": "1"}),
				expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
					"a": pendingDetail(), "b": pendingDetail(),
				}),
			},
		}},
	})
}

// TestAccMapOverwritePendingKey pins V2: a pending key re-supplied with a
// new value shows the new value in accumulated, stays out of outputs, and
// keeps its visible_at (the plan shows the entry as known, not fresh).
func TestAccMapOverwritePendingKey(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{})},
			},
			{
				Config: mapConfig(`{ a = "2" }`, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{}),
						expectMapPlanAccumulated(map[string]string{"a": "2"}),
						// The prior stamp is kept (D2), so the planned entry is known.
						expectPlanDetailed("accumulator_map.test", setDetail("a", pendingDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{}),
					expectMapAccumulated(map[string]string{"a": "2"}),
					expectDetailed("accumulator_map.test", setDetail("a", pendingDetail())),
				},
			},
		},
	})
}

// TestAccMapEnteringKeyHidden pins D1 and D5 on an update: existing visible
// keys stay in outputs; the new key appears in accumulated only.
func TestAccMapEnteringKeyHidden(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
			},
			{
				Config: mapConfig(`{ a = "1", b = "1" }`, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1"}),
						expectMapPlanAccumulated(map[string]string{"a": "1", "b": "1"}),
						expectPlanDetailVisibleUnknown("accumulator_map.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectMapAccumulated(map[string]string{"a": "1", "b": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": pendingDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapRevealRealizedOnRefresh pins V1 of the delay design: with no
// configuration change, the step's refresh reveals a before planning, so
// the plan is empty and outputs now holds it with its value.
func TestAccMapRevealRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mapConfig(`{ a = "1" }`, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{}),
					expectDetailed("accumulator_map.test", setDetail("a", pendingDetail())),
				},
			},
			{
				Config:    mapConfig(`{ a = "1" }`, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectMapAccumulated(map[string]string{"a": "1"}),
					expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccMapDelayedForRemoved pins T2: removing delayed_for reveals every
// pending key at that apply, and the plan shows the revealed outputs.
func TestAccMapDelayedForRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1"}),
						expectPlanDetailed("accumulator_map.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectDetailed("accumulator_map.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccMapDelayedForZeroRevealsNextRefresh pins delayed_for = 0: hidden
// at the apply that adds the key, revealed by the next refresh.
func TestAccMapDelayedForZeroRevealsNextRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  delayed_for = 0`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{})},
			},
			{
				Config:    mapConfig(`{ a = "1" }`, `  delayed_for = 0`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
			},
		},
	})
}

// TestAccMapDelayedForMustBeShorterThanExpiresAfter pins C1: equal and
// greater are rejected before any plan; shorter is accepted.
func TestAccMapDelayedForMustBeShorterThanExpiresAfter(t *testing.T) {
	for _, tc := range []struct{ name, extra string }{
		{"equal", "  delayed_for = 10\n  expires_after = 10"},
		{"greater", "  delayed_for = 11\n  expires_after = 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      mapConfig(`{ a = "1" }`, tc.extra),
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
				Config:            mapConfig(`{ a = "1" }`, "  delayed_for = 9\n  expires_after = 10"),
				ConfigStateChecks: []statecheck.StateCheck{expectMapAccumulated(map[string]string{"a": "1"})},
			}},
		})
	})
}

// TestAccMapPendingLeaverStamped pins T5: a key that leaves inputs while
// pending keeps visible_at and gains expires_at.
func TestAccMapPendingLeaverStamped(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1", b = "1" }`, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{}),
						expectMapPlanAccumulated(map[string]string{"a": "1", "b": "1"}),
						expectPlanDetailUnknown("accumulator_map.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": pendingDetail(), "b": pendingLeaverDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapResetDelaysSeed pins T4: a reset reseeds and every seeded key
// enters again with a full window, even one that was visible.
func TestAccMapResetDelaysSeed(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, "  delayed_for = 3600\n  triggers_reset = \"v2\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{}),
						expectMapPlanAccumulated(map[string]string{"a": "1"}),
						expectPlanDetailVisibleUnknown("accumulator_map.test", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{}),
					expectDetailed("accumulator_map.test", setDetail("a", pendingDetail())),
				},
			},
		},
	})
}

// TestAccMapUnknownDelayedForAtPlan pins the fallback path: an unknown
// delayed_for leaves outputs, accumulated, and detailed_outputs unknown at
// plan, and the apply resolves them with its own clock.
func TestAccMapUnknownDelayedForAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_map" "test" {
  inputs      = { a = "1" }
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
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("accumulated")),
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectMapOutputs(map[string]string{}),
				expectMapAccumulated(map[string]string{"a": "1"}),
				expectDetailed("accumulator_map.test", setDetail("a", pendingDetail())),
			},
		}},
	})
}
