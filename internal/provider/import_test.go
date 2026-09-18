// SPDX-License-Identifier: GPL-3.0-or-later

package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// importConfig supplies the required configuration for the resource being
// imported. The imported history comes from the ID, not from this config.
func importConfig(resourceType string) string {
	if resourceType == "accumulator_list" {
		return `
resource "accumulator_list" "test" {
  inputs = ["a", "b"]
  length = 3
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
				// empty.
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "c"),
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

// TestAccImportMalformed runs the three malformed shapes against both
// resources. Each ExpectError pattern is a provider-only phrase that does not
// appear in the configuration text, so the test cannot pass on OpenTofu's echo
// of the config.
func TestAccImportMalformed(t *testing.T) {
	for _, resourceType := range []string{"accumulator_list", "accumulator_set"} {
		resourceType := resourceType
		for label, tc := range map[string]struct {
			id     string
			expect *regexp.Regexp
		}{
			"bad json":         {`not-json`, regexp.MustCompile(`must be a JSON object`)},
			"unknown key":      {`{"outupts":["a"]}`, regexp.MustCompile(`not recognized`)},
			"wrong value type": {`{"outputs":"a"}`, regexp.MustCompile(`must be an array of strings`)},
		} {
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
