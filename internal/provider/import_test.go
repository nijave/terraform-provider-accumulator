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
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// importConfig supplies the required configuration for the resource being
// imported. The imported history comes from the ID, not from this config.
func importConfig(resourceType string) string {
	switch resourceType {
	case "accumulator_list":
		return `
resource "accumulator_list" "test" {
  inputs = ["a", "b"]
  length = 3
}
`
	case "accumulator_map":
		return `
resource "accumulator_map" "test" {
  inputs = { d = "4" }
}
`
	}
	return `
resource "accumulator_set" "test" {
  inputs = ["d"]
}
`
}

// TestAccListImport seeds inputs and outputs from the JSON ID. A following
// apply must be empty: the configuration's inputs match the seeded inputs, so
// the settling apply only fills in length and re-trims the seeded outputs.
func TestAccListImport(t *testing.T) {
	config := importConfig("accumulator_list")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_list.test",
				ImportState:        true,
				ImportStateId:      `{"inputs":["a","b"],"outputs":["a","b","c"]}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d resources, want 1", len(states))
					}
					attrs := states[0].Attributes
					// sha256(`["a","b"]`)
					if got := attrs["id"]; got != "0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13" {
						return fmt.Errorf("id = %q, want the hash of the seeded inputs", got)
					}
					if got := attrs["inputs.#"]; got != "2" {
						return fmt.Errorf("inputs.# = %q, want 2", got)
					}
					if got := attrs["outputs.#"]; got != "3" {
						return fmt.Errorf("outputs.# = %q, want 3", got)
					}
					if got := attrs["outputs.2"]; got != "c" {
						return fmt.Errorf("outputs.2 = %q, want \"c\"", got)
					}
					return nil
				},
			},
			{
				// The settling apply. length goes null -> 3 and outputs is
				// re-trimmed from the seeded value; after that the plan is
				// empty. Import seeds all-null detailed_outputs (spec
				// section 8), which is what keeps this plan empty.
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "c"),
					expectListAccumulated("a", "b", "c"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(), "c": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetImport seeds outputs from the JSON ID. inputs is seeded empty, and
// the settling apply unions the configured inputs into the seeded set.
func TestAccSetImport(t *testing.T) {
	config := importConfig("accumulator_set")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_set.test",
				ImportState:        true,
				ImportStateId:      `{"outputs":["a","b"]}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d resources, want 1", len(states))
					}
					attrs := states[0].Attributes
					// sha256(`["a","b"]`)
					if got := attrs["id"]; got != "0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13" {
						return fmt.Errorf("id = %q, want the hash of the seeded outputs", got)
					}
					if got := attrs["outputs.#"]; got != "2" {
						return fmt.Errorf("outputs.# = %q, want 2", got)
					}
					// inputs is a required attribute; import seeds it as an
					// empty list rather than null.
					if got := attrs["inputs.#"]; got != "0" {
						return fmt.Errorf("inputs.# = %q, want 0", got)
					}
					return nil
				},
			},
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b", "d"),
					expectSetAccumulated("a", "b", "d"),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetImportIgnoresInputsKey pins that an inputs key in a set import ID is
// accepted and ignored: the set resource seeds no meaningful inputs.
func TestAccSetImportIgnoresInputsKey(t *testing.T) {
	config := importConfig("accumulator_set")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_set.test",
				ImportState:        true,
				ImportStateId:      `{"inputs":["ignored"],"outputs":["a"]}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d resources, want 1", len(states))
					}
					attrs := states[0].Attributes
					if got := attrs["inputs.#"]; got != "0" {
						return fmt.Errorf("inputs.# = %q, want 0; the inputs key must be ignored", got)
					}
					return nil
				},
			},
			{
				// "ignored" must not appear in the set: only the seeded outputs
				// and the configured inputs are unioned.
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "d"),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetImportHashesDeduplicatedOutputs pins the import identity to the
// deduplicated outputs (design spec section 8): a seed containing duplicates
// must yield the same id as its deduplicated form, matching the state derived
// from it.
func TestAccSetImportHashesDeduplicatedOutputs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:             importConfig("accumulator_set"),
			ResourceName:       "accumulator_set.test",
			ImportState:        true,
			ImportStateId:      `{"outputs":["a","a","b"]}`,
			ImportStatePersist: true,
			ImportStateVerify:  false,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				if len(states) != 1 {
					return fmt.Errorf("imported %d resources, want 1", len(states))
				}
				attrs := states[0].Attributes
				if got := attrs["outputs.#"]; got != "2" {
					return fmt.Errorf("outputs.# = %q, want 2; the seed must be deduplicated", got)
				}
				// sha256(`["a","b"]`), the deduplicated outputs, not
				// sha256(`["a","a","b"]`).
				if got := attrs["id"]; got != "0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13" {
					return fmt.Errorf("id = %q, want the hash of the deduplicated outputs", got)
				}
				return nil
			},
		}},
	})
}

// TestAccMapImport seeds outputs from the JSON ID: id hashes the seeded
// map, inputs is an empty map, and the settling apply merges the
// configured inputs into the seeded outputs.
func TestAccMapImport(t *testing.T) {
	config := importConfig("accumulator_map")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_map.test",
				ImportState:        true,
				ImportStateId:      `{"outputs":{"a":"1","b":"1"}}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d resources, want 1", len(states))
					}
					attrs := states[0].Attributes
					// sha256(`{"a":"1","b":"1"}`)
					if got := attrs["id"]; got != "40239544dc03c3ec3a09cb947c72123697428a72af96b1c63431f34a46517709" {
						return fmt.Errorf("id = %q, want the hash of the seeded outputs", got)
					}
					if got := attrs["outputs.%"]; got != "2" {
						return fmt.Errorf("outputs.%% = %q, want 2", got)
					}
					if got := attrs["outputs.a"]; got != "1" {
						return fmt.Errorf("outputs.a = %q, want 1", got)
					}
					if got := attrs["inputs.%"]; got != "0" {
						return fmt.Errorf("inputs.%% = %q, want 0", got)
					}
					if got := attrs["detailed_outputs.%"]; got != "2" {
						return fmt.Errorf("detailed_outputs.%% = %q, want 2", got)
					}
					return nil
				},
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectMapPlanOutputs(map[string]string{"a": "1", "b": "1", "d": "4"})},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectMapOutputs(map[string]string{"a": "1", "b": "1", "d": "4"}),
					expectMapAccumulated(map[string]string{"a": "1", "b": "1", "d": "4"}),
				},
			},
		},
	})
}

// TestAccMapImportIgnoresInputsKey pins that an inputs key in the ID is
// accepted and ignored.
func TestAccMapImportIgnoresInputsKey(t *testing.T) {
	config := importConfig("accumulator_map")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_map.test",
				ImportState:        true,
				ImportStateId:      `{"inputs":{"zzz":"9"},"outputs":{"a":"1"}}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["inputs.%"]; got != "0" {
						return fmt.Errorf("inputs.%% = %q, want 0 (inputs key ignored)", got)
					}
					return nil
				},
			},
			{
				Config:            config,
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "d": "4"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccMapImportEmptyOutputs pins {"outputs":{}}: it seeds empty maps and
// the hash of "{}", and the first apply merges the config.
func TestAccMapImportEmptyOutputs(t *testing.T) {
	config := importConfig("accumulator_map")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_map.test",
				ImportState:        true,
				ImportStateId:      `{"outputs":{}}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					attrs := states[0].Attributes
					// sha256(`{}`)
					if got := attrs["id"]; got != "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
						return fmt.Errorf("id = %q, want the hash of {}", got)
					}
					if got := attrs["outputs.%"]; got != "0" {
						return fmt.Errorf("outputs.%% = %q, want 0", got)
					}
					return nil
				},
			},
			{
				Config:            config,
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"d": "4"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// importSeedsNullDetails is the ImportStateCheck shared by the import-then-
// expire tests: import seeds one detailed_outputs entry per seeded output,
// each with a null expires_at (expiry spec section 8). It inspects the raw
// flatmap because terraform-plugin-testing runs state checks only for Config
// steps, not import steps. Null object attributes are omitted from the
// flatmap, so each seeded value must have an entry object with both stamp
// attributes declared (expires_at and visible_at, so % is 2) and no
// non-empty stamp.
func importSeedsNullDetails(values ...string) func([]*terraform.InstanceState) error {
	return func(states []*terraform.InstanceState) error {
		if len(states) != 1 {
			return fmt.Errorf("imported %d resources, want 1", len(states))
		}
		attrs := states[0].Attributes
		if got := attrs["detailed_outputs.%"]; got != fmt.Sprint(len(values)) {
			return fmt.Errorf("detailed_outputs.%% = %q, want %d", got, len(values))
		}
		for _, value := range values {
			if got := attrs["detailed_outputs."+value+".%"]; got != "2" {
				return fmt.Errorf("detailed_outputs.%s.%% = %q, want 2", value, got)
			}
			path := "detailed_outputs." + value + ".expires_at"
			if got, ok := attrs[path]; ok && got != "" {
				return fmt.Errorf("%s = %q, want null", path, got)
			}
		}
		return nil
	}
}

// TestAccImportMalformed runs the malformed shapes against every resource.
// The list and set share the array shape; the map takes an object, so it
// also rejects the array shape the other two accept. Each ExpectError
// pattern is a provider-only phrase that does not appear in the
// configuration text, so the test cannot pass on OpenTofu's echo of the
// config.
func TestAccImportMalformed(t *testing.T) {
	type malformed struct {
		id     string
		expect *regexp.Regexp
	}
	arrayCases := map[string]malformed{
		"bad json":         {`not-json`, regexp.MustCompile(`must be a JSON object`)},
		"unknown key":      {`{"outupts":["a"]}`, regexp.MustCompile(`not recognized`)},
		"wrong value type": {`{"outputs":"a"}`, regexp.MustCompile(`must be an array of strings`)},
	}
	for resourceType, cases := range map[string]map[string]malformed{
		"accumulator_list": arrayCases,
		"accumulator_set":  arrayCases,
		"accumulator_map": {
			"bad json":     {`not-json`, regexp.MustCompile(`must be a JSON object`)},
			"unknown key":  {`{"outupts":{"a":"1"}}`, regexp.MustCompile(`not recognized`)},
			"array shape":  {`{"outputs":["a","b"]}`, regexp.MustCompile(`must be an object of strings`)},
			"number value": {`{"outputs":{"a":1}}`, regexp.MustCompile(`must be an object of strings`)},
		},
	} {
		resourceType := resourceType
		for label, tc := range cases {
			tc := tc
			t.Run(resourceType+"/"+label, func(t *testing.T) {
				resource.Test(t, resource.TestCase{
					PreCheck:                 func() { testAccPreCheck(t) },
					ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
					Steps: []resource.TestStep{{
						Config:            importConfig(resourceType),
						ResourceName:      resourceType + ".test",
						ImportState:       true,
						ImportStateId:     tc.id,
						ImportStateVerify: false,
						ExpectError:       tc.expect,
					}},
				})
			})
		}
	}
}
