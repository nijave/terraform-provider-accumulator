// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// coreModel holds the attributes the three resources share by name, type,
// and meaning. Each resource model embeds it by value; the framework
// promotes the embedded fields, so the tfsdk tags and the state shape are
// those of the flat models it replaced.
type coreModel struct {
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	DelayedFor          types.Int64  `tfsdk:"delayed_for"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
	ID                  types.String `tfsdk:"id"`
}

// importedCore is the coreModel of freshly imported state: no triggers, no
// expiry, no delay, the given detailed_outputs, and the given id.
func importedCore(detailed types.Map, id string) coreModel {
	return coreModel{
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		DelayedFor:          types.Int64Null(),
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(id),
	}
}
