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
					plancheck.ExpectUnknownValue("accumulator_map.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("accumulator_map.test",
					tfjsonpath.New("outputs"), knownvalue.MapSizeExact(1)),
				expectDetailed("accumulator_map.test", oneDetail("a", nullDetail())),
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
						expectPlanDetailed("accumulator_map.test", oneDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "2"}),
					expectDetailed("accumulator_map.test", oneDetail("a", nullDetail())),
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
						expectPlanDetailed("accumulator_map.test", oneDetail("a", pendingDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{}),
					expectMapAccumulated(map[string]string{"a": "2"}),
					expectDetailed("accumulator_map.test", oneDetail("a", pendingDetail())),
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
					expectDetailed("accumulator_map.test", oneDetail("a", pendingDetail())),
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
					expectDetailed("accumulator_map.test", oneDetail("a", nullDetail())),
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
						expectPlanDetailed("accumulator_map.test", oneDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectDetailed("accumulator_map.test", oneDetail("a", nullDetail())),
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
					expectDetailed("accumulator_map.test", oneDetail("a", pendingDetail())),
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
				expectDetailed("accumulator_map.test", oneDetail("a", pendingDetail())),
			},
		}},
	})
}

// TestAccMapImportThenDelayedFor pins 2026-10-18-accumulator-map-design.md
// section 9: imported keys seed visible, and the first new key in the
// settling apply is delayed.
func TestAccMapImportThenDelayedFor(t *testing.T) {
	config := `
resource "accumulator_map" "test" {
  inputs      = { d = "4" }
  delayed_for = 3600
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_map.test",
				ImportState:        true,
				ImportStateId:      `{"outputs":{"a":"1"}}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{"a": "1"}),
						expectMapPlanAccumulated(map[string]string{"a": "1", "d": "4"}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1"}),
					expectDetailed("accumulator_map.test", map[string]knownvalue.Check{
						"a": nullDetail(), "d": pendingDetail(),
					}),
				},
			},
		},
	})
}

// TestAccMapDelayedForDecreaseRevealsSooner pins that a pending key stamped
// +1h; decreasing to 1 recalculates min(old, now+1s) at apply, and the
// following refresh reveals it. Step 2 leaves a 1s stamp pending, so it
// carries no post-refresh check.
func TestAccMapDelayedForDecreaseRevealsSooner(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  delayed_for = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{}),
						expectPlanDetailVisibleUnknown("accumulator_map.test", "a"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectDetailed("accumulator_map.test", oneDetail("a", pendingDetail())),
				},
			},
			{
				Config:    mapConfig(`{ a = "1" }`, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1"})},
			},
		},
	})
}

// TestAccMapDelayedForIncreaseKeepsPending pins D4: an increase restamps
// the pending key (fresh, unknown in the plan) and it stays hidden.
func TestAccMapDelayedForIncreaseKeepsPending(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            mapConfig(`{ a = "1" }`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{})},
			},
			{
				Config: mapConfig(`{ a = "1" }`, `  delayed_for = 7200`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectMapPlanOutputs(map[string]string{}),
						expectPlanDetailVisibleUnknown("accumulator_map.test", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{}),
					expectDetailed("accumulator_map.test", oneDetail("a", pendingDetail())),
				},
			},
		},
	})
}

// TestAccMapEmptyStringKey pins that "" is a legal map key: it is stored,
// revealed, and stable across a refresh.
func TestAccMapEmptyStringKey(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: mapConfig(`{ "" = "1" }`, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				expectMapOutputs(map[string]string{"": "1"}),
				expectDetailed("accumulator_map.test", oneDetail("", nullDetail())),
			},
			ConfigPlanChecks: expectEmptyAfterRefresh(),
		}},
	})
}
