// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// flavor adapts the shared expiry and delay matrix to one resource type.
// Every row names values by identity only: a value of a list or set, a key
// of a map. A row writes each expected collection in list semantics (order
// kept, duplicates kept), which is the most specific of the three; the set
// and map flavors deduplicate it, and the map flavor pairs every key with
// the value mapValue.
type flavor struct {
	// name is the subtest name: "list", "set", or "map".
	name string
	// addr is the resource address under test, such as "accumulator_set.test".
	addr string
	// config renders the resource under test with the given identities as
	// inputs. extra is a raw attribute line or two, "" for none.
	config func(ids []string, extra string) string
	// values renders the knownvalue check for a whole outputs or
	// accumulated value.
	values func(ids []string) knownvalue.Check
	// importID renders an import ID seeding the given outputs. inputs is
	// seeded only by the list flavor, which compares inputs; the set and
	// map flavors drop it, matching their ImportState. A nil inputs leaves
	// the key out.
	importID func(inputs, outputs []string) string
}

// mapValue is the value the map flavor gives every key. Values take no part
// in expiry or delay (2026-10-18-accumulator-map-design.md section 3), so a
// constant keeps the rows about identity.
const mapValue = "1"

// sharedListLength is the list flavor's length: long enough that no row
// trims, so the list rows test stamps rather than trimming.
const sharedListLength = 10

var listFlavor = flavor{
	name: "list",
	addr: "accumulator_list.test",
	config: func(ids []string, extra string) string {
		return listConfig(hclList(ids), sharedListLength, extra)
	},
	values: func(ids []string) knownvalue.Check {
		return knownvalue.ListExact(stringList(ids...))
	},
	importID: func(inputs, outputs []string) string {
		if inputs == nil {
			return fmt.Sprintf(`{"outputs":%s}`, jsonArray(outputs))
		}
		return fmt.Sprintf(`{"inputs":%s,"outputs":%s}`, jsonArray(inputs), jsonArray(outputs))
	},
}

var setFlavor = flavor{
	name: "set",
	addr: "accumulator_set.test",
	config: func(ids []string, extra string) string {
		return setConfig(hclList(ids), extra)
	},
	values: func(ids []string) knownvalue.Check {
		return knownvalue.SetExact(stringList(unique(ids)...))
	},
	importID: func(_, outputs []string) string {
		return fmt.Sprintf(`{"outputs":%s}`, jsonArray(outputs))
	},
}

var mapFlavor = flavor{
	name: "map",
	addr: "accumulator_map.test",
	config: func(ids []string, extra string) string {
		return mapConfig(hclMap(ids), extra)
	},
	values: func(ids []string) knownvalue.Check {
		return knownvalue.MapExact(stringMap(mapOf(ids)))
	},
	importID: func(_, outputs []string) string {
		encoded, err := json.Marshal(mapOf(outputs))
		if err != nil {
			panic(err)
		}
		return fmt.Sprintf(`{"outputs":%s}`, encoded)
	},
}

// flavors is every resource the matrix runs against.
var flavors = []flavor{listFlavor, setFlavor, mapFlavor}

func (f flavor) expectOutputs(ids ...string) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(f.addr, tfjsonpath.New("outputs"), f.values(ids))
}

func (f flavor) expectAccumulated(ids ...string) statecheck.StateCheck {
	return expectAccumulated(f.addr, f.values(ids))
}

func (f flavor) expectPlanOutputs(ids ...string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue(f.addr, tfjsonpath.New("outputs"), f.values(ids))
}

func (f flavor) expectPlanAccumulated(ids ...string) plancheck.PlanCheck {
	return expectPlanAccumulated(f.addr, f.values(ids))
}

// unique drops repeated identities, keeping the first occurrence.
func unique(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// mapOf pairs every identity with mapValue.
func mapOf(ids []string) map[string]string {
	m := make(map[string]string, len(ids))
	for _, id := range ids {
		m[id] = mapValue
	}
	return m
}

// hclList renders identities as an HCL list literal, such as ["a", "b"].
func hclList(ids []string) string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, fmt.Sprintf("%q", id))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// hclMap renders identities as an HCL map literal with every value set to
// mapValue, such as { "a" = "1", "b" = "1" }.
func hclMap(ids []string) string {
	if len(ids) == 0 {
		return "{}"
	}
	entries := make([]string, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, fmt.Sprintf("%q = %q", id, mapValue))
	}
	return "{ " + strings.Join(entries, ", ") + " }"
}

// jsonArray renders identities as a JSON array of strings.
func jsonArray(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// ids is shorthand for an identity list in a row.
func ids(values ...string) []string { return values }

// unknownSource is an upstream resource whose outputs are never known at
// plan time (uuid), so any attribute derived from it is unknown in the plan
// of the resource under test. It diffs forever, so a row that uses it sets
// ExpectNonEmptyPlan and carries no post-refresh check.
const unknownSource = `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}
`

// sharedCase is one row of the matrix: a named test whose steps are built
// for each flavor in turn.
type sharedCase struct {
	name  string
	steps func(f flavor) []resource.TestStep
}

// TestAccShared runs every expiry and delay behavior that depends only on
// identity against all three resources, one subtest per row and flavor:
// TestAccShared/<row>/<flavor>. Behavior that depends on the collection
// type (list order, length, and duplicates; set deduplication; map value
// overwrite; import shapes) stays in the per-resource files.
func TestAccShared(t *testing.T) {
	for _, group := range sharedGroups {
		for _, tc := range group {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				for _, f := range flavors {
					f := f
					t.Run(f.name, func(t *testing.T) {
						resource.Test(t, resource.TestCase{
							PreCheck:                 func() { testAccPreCheck(t) },
							ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
							Steps:                    tc.steps(f),
						})
					})
				}
			})
		}
	}
}

// sharedGroups is every group of rows TestAccShared runs.
var sharedGroups = [][]sharedCase{expiryCases, delayCases}

// expiryCases are the expires_after rows. Each row's comment names the rule
// it pins (2026-09-23-accumulator-expiration-design.md rule IDs).
var expiryCases = []sharedCase{
	// E1. The create shape: every value is in inputs, so detailed_outputs is
	// fully known at plan time and all null.
	{"CreateWithExpiresAfter", func(f flavor) []resource.TestStep {
		return []resource.TestStep{{
			Config: f.config(ids("a", "b"), `  expires_after = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					f.expectPlanOutputs("a", "b"),
					expectPlanDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				f.expectOutputs("a", "b"),
				expectDetailed(f.addr, map[string]knownvalue.Check{
					"a": nullDetail(), "b": nullDetail(),
				}),
			},
		}}
	}},

	// E2. S1: b leaves inputs and is stamped at apply; a stays null. b's
	// entry is fresh, so it is unknown in the plan. The 3600s window keeps
	// the post-refresh plan empty without racing the clock.
	{"LeaverStampedOnInputsChange", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: f.config(ids("a"), `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "b", "a"),
						expectPlanDetailedPartial(f.addr, oneDetail("a", nullDetail())),
						expectPlanDetailUnknown(f.addr, "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b", "a"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		}
	}},

	// E3. X1: with no configuration change, the step's refresh culls b
	// before planning, so the plan is empty and state no longer holds it.
	// Step 2 leaves a 1s stamp pending, so it carries no post-refresh check.
	{"ExpiryRealizedOnRefresh", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config: f.config(ids("a"), `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b", "a"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config:    f.config(ids("a"), `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan, so the plan
					// proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "a"),
					f.expectAccumulated("a", "a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// E4. R2 and the steady-state invariant: with expires_after = 1 held
	// over a sleep, a stays unstamped and unremoved, and the plan after the
	// sleep is empty.
	{"InInputsNeverExpires", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a"), `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a")},
			},
			{
				Config:    f.config(ids("a"), `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// E5. S5: history accumulated with no expires_after gets a full window
	// when it is enabled. b is fresh history; its expires_at is unknown
	// until the apply stamps it.
	{"ExpiresAfterSetOnExistingResource", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config: f.config(ids("a", "b"), ""),
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: f.config(ids("a"), `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "b", "a"),
						expectPlanDetailedPartial(f.addr, oneDetail("a", nullDetail())),
						expectPlanDetailUnknown(f.addr, "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b", "a"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		}
	}},

	// E6. T2: removing expires_after expires nothing and nulls every
	// expires_at.
	{"ExpiresAfterRemoved", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config:            f.config(ids("a"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b", "a")},
			},
			{
				Config: f.config(ids("a"), ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// T2 removes nothing: b survives the removal of
						// expires_after, with its stamp nulled.
						f.expectPlanOutputs("a", "b", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b", "a"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		}
	}},

	// E7. X1's E=0 case: b's entry is fresh in the E-zero plan and the apply
	// stamps it with the current moment, so the next refresh culls it.
	{"ExpiresAfterZeroCullsNextRefresh", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config:            f.config(ids("a"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b", "a")},
			},
			{
				Config: f.config(ids("a"), `  expires_after = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "b", "a"),
						// b's stamp is fresh (a decrease to 0), unknown until
						// the apply stamps it "now".
						expectPlanDetailedPartial(f.addr, oneDetail("a", nullDetail())),
						expectPlanDetailUnknown(f.addr, "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// The apply stamps b with the current moment; the refresh
					// that culls it runs after these checks.
					f.expectOutputs("a", "b", "a"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: f.config(ids("a"), `  expires_after = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan.
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// E8. S3 end to end: b was stamped +1h; decreasing to 1 recalculates
	// min(old, ~now+1s) at apply, and the following refresh culls it. Step 3
	// leaves a 1s stamp pending, so it carries no post-refresh check.
	{"ExpiresAfterDecreasePullsEarlier", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config:            f.config(ids("a"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b", "a")},
			},
			{
				Config: f.config(ids("a"), `  expires_after = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "b", "a"),
						// A decrease recalculates b's stamp (min(old,
						// candidate)), so it is fresh and unknown until the
						// apply.
						expectPlanDetailedPartial(f.addr, oneDetail("a", nullDetail())),
						expectPlanDetailUnknown(f.addr, "b"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b", "a"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config:    f.config(ids("a"), `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// The refresh culls b before the plan, so the plan
					// proposes nothing.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// E9. The expiry spec's accepted X1 consequence: b's 1s window has
	// passed by step 3's refresh, which culls it before the plan can apply
	// S4, so raising expires_after cannot bring it back. Step 2 leaves the
	// 1s stamp pending, so it carries no post-refresh check.
	{"IncreaseDoesNotResurrect", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config:            f.config(ids("a"), `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b", "a")},
			},
			{
				Config:    f.config(ids("a"), `  expires_after = 3600`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// b was culled at refresh; the plan only shows a.
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "a"),
						expectPlanDetailed(f.addr, oneDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// E10. S4: an expires_after increase re-times a still-live value to
	// max(old, candidate) and keeps it in outputs. The 300s first window
	// keeps b alive across the test; b is fresh under each change, so it is
	// unknown in each of those plans.
	{"ExpiresAfterIncreaseKeepsLive", func(f flavor) []resource.TestStep {
		leaverPlan := func() []plancheck.PlanCheck {
			return []plancheck.PlanCheck{
				f.expectPlanOutputs("a", "b", "a"),
				expectPlanDetailedPartial(f.addr, oneDetail("a", nullDetail())),
				expectPlanDetailUnknown(f.addr, "b"),
			}
		}
		leaverState := func() []statecheck.StateCheck {
			return []statecheck.StateCheck{
				f.expectOutputs("a", "b", "a"),
				expectDetailed(f.addr, map[string]knownvalue.Check{
					"a": nullDetail(), "b": stampedDetail(),
				}),
			}
		}
		return []resource.TestStep{
			{
				Config: f.config(ids("a", "b"), `  expires_after = 300`),
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: f.config(ids("a"), `  expires_after = 300`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             leaverPlan(),
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: leaverState(),
			},
			{
				// b survives the increase and is re-stamped, so it is still
				// present and its entry is unknown until the apply.
				Config: f.config(ids("a"), `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             leaverPlan(),
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: leaverState(),
			},
		}
	}},

	// E11. T5: b's 1s window has passed unrealized, and re-adding b to
	// inputs rescues it with a null stamp. Step 2 leaves the 1s stamp
	// pending, so it carries no post-refresh check. The list appends, so
	// its history is a, b, a, then a, a after the cull, then a, a, a, b.
	{"ReSupplyRescues", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config:            f.config(ids("a"), `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b", "a")},
			},
			{
				Config:    f.config(ids("a", "b"), `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "a", "a", "b"),
						expectPlanDetailed(f.addr, map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "a", "a", "b"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		}
	}},

	// E12. T4: a reset reseeds from inputs alone, so detailed_outputs is all
	// null again and stamped leavers are forgotten.
	{"ResetClearsStamps", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), "  expires_after = 3600\n  triggers_reset = \"v1\""),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config: f.config(ids("a"), "  expires_after = 3600\n  triggers_reset = \"v1\""),
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b", "a"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
			{
				Config: f.config(ids("a"), "  expires_after = 3600\n  triggers_reset = \"v2\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a"),
						expectPlanDetailed(f.addr, oneDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// E13. T4's replacement half: a triggers_replacement change destroys and
	// recreates, Create reseeds from inputs alone, and detailed_outputs is
	// all null again.
	{"ReplacementClearsStamps", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), "  expires_after = 3600\n  triggers_replacement = \"v1\""),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b")},
			},
			{
				Config:            f.config(ids("a"), "  expires_after = 3600\n  triggers_replacement = \"v1\""),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a", "b", "a")},
			},
			{
				Config: f.config(ids("a"), "  expires_after = 3600\n  triggers_replacement = \"v2\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(f.addr, plancheck.ResourceActionReplace),
						f.expectPlanOutputs("a"),
						expectPlanDetailed(f.addr, oneDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// E14. The fallback path: expires_after derived from an upstream unknown
	// stays unknown at plan, so outputs and detailed_outputs plan as unknown
	// and the apply resolves them with the apply clock.
	{"UnknownExpiresAfterAtPlan", func(f flavor) []resource.TestStep {
		return []resource.TestStep{{
			Config:             unknownSource + f.config(ids("a"), `  expires_after = length(accumulator_list.src.outputs)`),
			ExpectNonEmptyPlan: true,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue(f.addr, tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue(f.addr, tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				f.expectOutputs("a"),
				expectDetailed(f.addr, oneDetail("a", nullDetail())),
			},
		}}
	}},

	// E15. Expiry spec section 8: the import ID carries no expiration data,
	// imported entries are null, and enabling expires_after in the settling
	// apply stamps non-inputs values via S5. The list seeds inputs so the
	// settling apply does not append them again.
	{"ImportThenExpiresAfter", func(f flavor) []resource.TestStep {
		config := f.config(ids("a"), `  expires_after = 3600`)
		return []resource.TestStep{
			{
				Config:             config,
				ResourceName:       f.addr,
				ImportState:        true,
				ImportStateId:      f.importID(ids("a"), ids("a", "b")),
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
						f.expectPlanOutputs("a", "b"),
						expectPlanDetailedPartial(f.addr, oneDetail("a", nullDetail())),
						expectPlanDetailUnknown(f.addr, "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		}
	}},

	// E16. The validation upper bound: a value above maxExpiresAfter
	// (9223372036) is rejected at plan time by the between-validator.
	{"ExpiresAfterUpperBound", func(f flavor) []resource.TestStep {
		return []resource.TestStep{{
			Config:      f.config(ids("a"), `  expires_after = 10000000000`),
			ExpectError: regexp.MustCompile(`value must be between`),
		}}
	}},
}

// delayCases are the delayed_for rows. Each row's comment names the rule it
// pins (2026-10-15-accumulator-delayed-for-design.md rule IDs).
var delayCases = []sharedCase{
	// D1. D1 on create: outputs plans as known and empty, accumulated as
	// known and seeded, and every visible_at is unknown until the apply
	// stamps it. The 3600s window keeps the post-refresh plan empty.
	{"CreateWithDelayedFor", func(f flavor) []resource.TestStep {
		return []resource.TestStep{{
			Config: f.config(ids("a", "b"), `  delayed_for = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					f.expectPlanOutputs(),
					f.expectPlanAccumulated("a", "b"),
					expectPlanDetailVisibleUnknown(f.addr, "a"),
					expectPlanDetailVisibleUnknown(f.addr, "b"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				f.expectOutputs(),
				f.expectAccumulated("a", "b"),
				expectDetailed(f.addr, map[string]knownvalue.Check{
					"a": pendingDetail(), "b": pendingDetail(),
				}),
			},
		}}
	}},

	// D2. Update, reset, and replacement all plan outputs and accumulated as
	// known while delayed_for is set. On the update a stays visible (D5)
	// and b enters and hides (D1); the list appends, so its history repeats
	// a. Reset and replacement reseed and every value enters (T4).
	{"OutputsKnownAtPlanTimeWithDelayedFor", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a"), ""),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a")},
			},
			{
				Config: f.config(ids("a", "b"), `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "a"),
						f.expectPlanAccumulated("a", "a", "b"),
						expectPlanDetailVisibleUnknown(f.addr, "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "a"),
					f.expectAccumulated("a", "a", "b"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": pendingDetail(),
					}),
				},
			},
			{
				Config: f.config(ids("a", "b"), "  delayed_for = 3600\n  triggers_reset = \"v1\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs(),
						f.expectPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs(), f.expectAccumulated("a", "b")},
			},
			{
				Config: f.config(ids("a", "b"), "  delayed_for = 3600\n  triggers_reset = \"v1\"\n  triggers_replacement = \"r1\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(f.addr, plancheck.ResourceActionReplace),
						f.expectPlanOutputs(),
						f.expectPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs(), f.expectAccumulated("a", "b")},
			},
		}
	}},

	// D3. V1: with no configuration change, the step's refresh reveals a
	// before planning, so the plan is empty and outputs now holds it with a
	// null visible_at.
	{"RevealRealizedOnRefresh", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config: f.config(ids("a"), `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs(),
					expectDetailed(f.addr, oneDetail("a", pendingDetail())),
				},
			},
			{
				Config:    f.config(ids("a"), `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a"),
					f.expectAccumulated("a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// D4. T2: removing delayed_for reveals every pending value at that
	// apply, and the plan shows the revealed outputs.
	{"DelayedForRemoved", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs()},
			},
			{
				Config: f.config(ids("a", "b"), ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "b"),
						expectPlanDetailed(f.addr, map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		}
	}},

	// D5. D3 end to end: a was stamped +1h; decreasing to 1 recalculates
	// min(old, now+1s) at apply, and the following refresh reveals it. Step
	// 2 leaves a 1s stamp pending, so it carries no post-refresh check.
	{"DelayedForDecreaseRevealsSooner", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a"), `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs()},
			},
			{
				Config: f.config(ids("a"), `  delayed_for = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs(),
						expectPlanDetailVisibleUnknown(f.addr, "a"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectDetailed(f.addr, oneDetail("a", pendingDetail())),
				},
			},
			{
				Config:    f.config(ids("a"), `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a")},
			},
		}
	}},

	// D6. D4: an increase restamps the pending value (fresh, unknown in the
	// plan) and it stays hidden.
	{"DelayedForIncreaseKeepsPending", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a"), `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs()},
			},
			{
				Config: f.config(ids("a"), `  delayed_for = 7200`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs(),
						expectPlanDetailVisibleUnknown(f.addr, "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs(),
					expectDetailed(f.addr, oneDetail("a", pendingDetail())),
				},
			},
		}
	}},

	// D7. The delayed_for = 0 reading of V1: the apply stamps the current
	// moment and leaves outputs unchanged; the next refresh reveals, with no
	// sleep needed.
	{"DelayedForZeroRevealsNextRefresh", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config: f.config(ids("a"), `  delayed_for = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{f.expectPlanOutputs()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// The apply stamps a with the current moment; the refresh
					// that reveals it runs after these checks.
					f.expectOutputs(),
					expectDetailed(f.addr, oneDetail("a", pendingDetail())),
				},
			},
			{
				Config: f.config(ids("a"), `  delayed_for = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a"),
					expectDetailed(f.addr, oneDetail("a", nullDetail())),
				},
			},
		}
	}},

	// D8. C1: equal and greater are rejected before any plan; shorter is
	// accepted. The rejected steps create nothing, so the accepted step is
	// a create.
	{"DelayedForMustBeShorterThanExpiresAfter", func(f flavor) []resource.TestStep {
		c1 := regexp.MustCompile(`delayed_for must be shorter than expires_after`)
		return []resource.TestStep{
			{
				Config:      f.config(ids("a"), "  delayed_for = 10\n  expires_after = 10"),
				ExpectError: c1,
			},
			{
				Config:      f.config(ids("a"), "  delayed_for = 11\n  expires_after = 10"),
				ExpectError: c1,
			},
			{
				Config:            f.config(ids("a"), "  delayed_for = 9\n  expires_after = 10"),
				ConfigStateChecks: []statecheck.StateCheck{f.expectAccumulated("a")},
			},
		}
	}},

	// D9. T5: a value that leaves inputs while pending keeps visible_at and
	// gains expires_at.
	{"PendingLeaverStamped", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a", "b"), "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs()},
			},
			{
				Config: f.config(ids("a"), "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs(),
						f.expectPlanAccumulated("a", "b", "a"),
						expectPlanDetailUnknown(f.addr, "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs(),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": pendingDetail(), "b": pendingLeaverDetail(),
					}),
				},
			},
		}
	}},

	// D10. T4 with a value that was already visible: a reset reseeds and the
	// seeded value enters again with a full window.
	{"ResetDelaysSeed", func(f flavor) []resource.TestStep {
		return []resource.TestStep{
			{
				Config:            f.config(ids("a"), `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{f.expectOutputs("a")},
			},
			{
				Config: f.config(ids("a"), "  delayed_for = 3600\n  triggers_reset = \"v2\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs(),
						f.expectPlanAccumulated("a"),
						expectPlanDetailVisibleUnknown(f.addr, "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs(),
					expectDetailed(f.addr, oneDetail("a", pendingDetail())),
				},
			},
		}
	}},

	// D11. The fallback path: an unknown delayed_for leaves outputs,
	// accumulated, and detailed_outputs unknown at plan, and the apply
	// resolves them with its own clock.
	{"UnknownDelayedForAtPlan", func(f flavor) []resource.TestStep {
		return []resource.TestStep{{
			Config:             unknownSource + f.config(ids("a"), `  delayed_for = length(accumulator_list.src.outputs) * 3600`),
			ExpectNonEmptyPlan: true,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue(f.addr, tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue(f.addr, tfjsonpath.New("accumulated")),
					plancheck.ExpectUnknownValue(f.addr, tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				f.expectOutputs(),
				f.expectAccumulated("a"),
				expectDetailed(f.addr, oneDetail("a", pendingDetail())),
			},
		}}
	}},

	// D12. Delay spec section 8: imported values seed visible, and the first
	// new value in the settling apply is delayed. The list seeds no inputs,
	// so the settling apply appends d.
	{"ImportThenDelayedFor", func(f flavor) []resource.TestStep {
		config := f.config(ids("d"), `  delayed_for = 3600`)
		return []resource.TestStep{
			{
				Config:             config,
				ResourceName:       f.addr,
				ImportState:        true,
				ImportStateId:      f.importID(nil, ids("a", "b")),
				ImportStatePersist: true,
				ImportStateVerify:  false,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						f.expectPlanOutputs("a", "b"),
						f.expectPlanAccumulated("a", "b", "d"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					f.expectOutputs("a", "b"),
					expectDetailed(f.addr, map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(), "d": pendingDetail(),
					}),
				},
			},
		}
	}},
}
