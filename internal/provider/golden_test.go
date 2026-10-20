// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// updateGolden rewrites testdata/phases.golden from the current code. Run it
// only on a tree whose behavior is the reference:
//
//	go test ./internal/provider/ -run TestPhasesGolden -update
var updateGolden = flag.Bool("update", false, "rewrite testdata/phases.golden")

const goldenPath = "testdata/phases.golden"

// Fixture stamps. Every stamp a scenario puts in state is one of these, so
// any other RFC 3339 value in an output came from the apply or refresh
// clock and is masked as <clock>.
const (
	pastStamp   = "2000-01-01T00:00:00Z"
	futureStamp = "2999-01-01T00:00:00Z"
)

// TestPhasesGolden pins every value the three resources plan, apply, read,
// import, and validate, byte for byte, against testdata/phases.golden. Each
// scenario runs against all three resources. ModifyPlan reads no clock, so
// its output is exact; Create, Update, and Read stamp with the clock, so
// clock-derived timestamps are masked and every other byte is exact.
func TestPhasesGolden(t *testing.T) {
	var b strings.Builder
	for _, f := range goldFlavors() {
		for _, c := range goldPlanCases {
			fmt.Fprintf(&b, "== %s / plan / %s\n", f.name, c.name)
			b.WriteString(runPlanCase(t, f, c))
		}
		for _, c := range goldReadCases {
			fmt.Fprintf(&b, "== %s / read / %s\n", f.name, c.name)
			b.WriteString(runReadCase(t, f, c))
		}
		for _, c := range goldImportCases(f) {
			fmt.Fprintf(&b, "== %s / import / %s\n", f.name, c.name)
			b.WriteString(runImportCase(t, f, c.id))
		}
		for _, c := range goldValidateCases {
			fmt.Fprintf(&b, "== %s / validate / %s\n", f.name, c.name)
			b.WriteString(runValidateCase(t, f, c))
		}
	}
	got := b.String()

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading %s (generate it with -update on the reference tree): %v", goldenPath, err)
	}
	if got != string(want) {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
			var g, w string
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if g != w {
				t.Fatalf("%s differs at line %d:\n got: %s\nwant: %s", goldenPath, i+1, g, w)
			}
		}
	}
}

// goldFlavor builds raw values of one resource's schema from identity lists.
type goldFlavor struct {
	name string
	res  resource.Resource
	sch  schema.Schema
	// inputs renders configured inputs: a list, or a map with every value "1".
	inputs func(ids ...string) tftypes.Value
	// coll renders accumulated or outputs: a list, a set, or a map.
	coll func(ids ...string) tftypes.Value
	// unknownInput is inputs holding one unknown element.
	unknownInput func() tftypes.Value
	// fixed holds attributes only this resource has (the list's length).
	fixed map[string]tftypes.Value
}

var (
	goldString     = tftypes.String
	goldListType   = tftypes.List{ElementType: tftypes.String}
	goldSetType    = tftypes.Set{ElementType: tftypes.String}
	goldMapType    = tftypes.Map{ElementType: tftypes.String}
	goldDetailType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"expires_at": tftypes.String, "visible_at": tftypes.String,
	}}
)

func goldStrings(ids []string) []tftypes.Value {
	out := make([]tftypes.Value, 0, len(ids))
	for _, id := range ids {
		out = append(out, tftypes.NewValue(goldString, id))
	}
	return out
}

func goldUnique(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func goldMap(ids []string) tftypes.Value {
	m := make(map[string]tftypes.Value, len(ids))
	for _, id := range ids {
		m[id] = tftypes.NewValue(goldString, "1")
	}
	return tftypes.NewValue(goldMapType, m)
}

func goldFlavors() []goldFlavor {
	list := func(ids ...string) tftypes.Value { return tftypes.NewValue(goldListType, goldStrings(ids)) }
	unknownList := func() tftypes.Value {
		return tftypes.NewValue(goldListType, []tftypes.Value{tftypes.NewValue(goldString, tftypes.UnknownValue)})
	}
	return []goldFlavor{
		{
			name: "list", res: &listResource{},
			inputs: list, coll: list, unknownInput: unknownList,
			fixed: map[string]tftypes.Value{"length": tftypes.NewValue(tftypes.Number, 10)},
		},
		{
			name: "set", res: &setResource{},
			inputs: list,
			coll: func(ids ...string) tftypes.Value {
				return tftypes.NewValue(goldSetType, goldStrings(goldUnique(ids)))
			},
			unknownInput: unknownList,
		},
		{
			name: "map", res: &mapResource{},
			inputs: func(ids ...string) tftypes.Value { return goldMap(ids) },
			coll:   func(ids ...string) tftypes.Value { return goldMap(ids) },
			unknownInput: func() tftypes.Value {
				return tftypes.NewValue(goldMapType, map[string]tftypes.Value{"a": tftypes.NewValue(goldString, tftypes.UnknownValue)})
			},
		},
	}
}

// goldStr, goldNum, and goldUnknown are scalar attribute values.
func goldStr(s string) tftypes.Value { return tftypes.NewValue(goldString, s) }
func goldNum(n int64) tftypes.Value  { return tftypes.NewValue(tftypes.Number, n) }
func goldUnknown(t tftypes.Type) tftypes.Value {
	return tftypes.NewValue(t, tftypes.UnknownValue)
}

// goldDetail renders detailed_outputs. Each entry is "expires_at,visible_at"
// where an empty half is null and "?" is unknown.
func goldDetail(entries map[string]string) tftypes.Value {
	m := make(map[string]tftypes.Value, len(entries))
	for k, v := range entries {
		halves := strings.SplitN(v, ",", 2)
		attrs := map[string]tftypes.Value{}
		for i, name := range []string{"expires_at", "visible_at"} {
			switch halves[i] {
			case "":
				attrs[name] = tftypes.NewValue(goldString, nil)
			case "?":
				attrs[name] = goldUnknown(goldString)
			default:
				attrs[name] = goldStr(halves[i])
			}
		}
		m[k] = tftypes.NewValue(goldDetailType, attrs)
	}
	return tftypes.NewValue(tftypes.Map{ElementType: goldDetailType}, m)
}

// goldObject fills every attribute the scenario did not name with null (or,
// for a plan, with what the framework proposes for computed attributes:
// unknown, and the id from state).
func goldObject(ctx context.Context, f goldFlavor, attrs map[string]tftypes.Value, proposed bool, stateID tftypes.Value) tftypes.Value {
	typ := f.sch.Type().TerraformType(ctx).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, t := range typ.AttributeTypes {
		if v, ok := attrs[name]; ok {
			vals[name] = v
			continue
		}
		if v, ok := f.fixed[name]; ok {
			vals[name] = v
			continue
		}
		switch {
		case proposed && (name == "accumulated" || name == "outputs" || name == "detailed_outputs"):
			vals[name] = goldUnknown(t)
		case proposed && name == "id":
			vals[name] = stateID
		default:
			vals[name] = tftypes.NewValue(t, nil)
		}
	}
	return tftypes.NewValue(typ, vals)
}

// goldPlanCase is a ModifyPlan scenario. plan holds configured attributes
// (nil for a destroy plan); state holds prior state (nil on create). When
// apply is true and the plan comes back without error, the scenario also
// runs the apply it implies (Create on create or replacement, otherwise
// Update) on the known path with the planned value, and on the fallback
// path with the proposed value. replace marks a triggers_replacement change,
// which Terraform applies as Delete then Create.
type goldPlanCase struct {
	name    string
	plan    func(f goldFlavor) map[string]tftypes.Value
	state   func(f goldFlavor) map[string]tftypes.Value
	apply   bool
	replace bool
}

// goldState is a fully visible, unstamped prior state for ids with the given
// configuration attributes merged in.
func goldState(f goldFlavor, ids []string, extra map[string]tftypes.Value) map[string]tftypes.Value {
	entries := map[string]string{}
	for _, id := range ids {
		entries[id] = ","
	}
	m := map[string]tftypes.Value{
		"inputs":           f.inputs(ids...),
		"accumulated":      f.coll(ids...),
		"outputs":          f.coll(ids...),
		"detailed_outputs": goldDetail(entries),
		"id":               goldStr("state-id"),
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

var goldPlanCases = []goldPlanCase{
	{name: "destroy", state: func(f goldFlavor) map[string]tftypes.Value { return goldState(f, []string{"a"}, nil) }},
	{name: "create", apply: true, plan: func(f goldFlavor) map[string]tftypes.Value {
		return map[string]tftypes.Value{"inputs": f.inputs("a", "b")}
	}},
	{name: "create empty", apply: true, plan: func(f goldFlavor) map[string]tftypes.Value {
		return map[string]tftypes.Value{"inputs": f.inputs()}
	}},
	{name: "create with expires_after", apply: true, plan: func(f goldFlavor) map[string]tftypes.Value {
		return map[string]tftypes.Value{"inputs": f.inputs("a", "b"), "expires_after": goldNum(3600)}
	}},
	{name: "create with delayed_for", apply: true, plan: func(f goldFlavor) map[string]tftypes.Value {
		return map[string]tftypes.Value{"inputs": f.inputs("a", "b"), "delayed_for": goldNum(3600)}
	}},
	{name: "update adds", apply: true,
		plan:  func(f goldFlavor) map[string]tftypes.Value { return map[string]tftypes.Value{"inputs": f.inputs("b")} },
		state: func(f goldFlavor) map[string]tftypes.Value { return goldState(f, []string{"a"}, nil) },
	},
	{name: "update unchanged", apply: true,
		plan:  func(f goldFlavor) map[string]tftypes.Value { return map[string]tftypes.Value{"inputs": f.inputs("a")} },
		state: func(f goldFlavor) map[string]tftypes.Value { return goldState(f, []string{"a"}, nil) },
	},
	{name: "leaver stamped", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "expires_after": goldNum(3600)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{"expires_after": goldNum(3600)})
		},
	},
	{name: "expires_after decrease", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "expires_after": goldNum(1)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{
				"inputs": f.inputs("a"), "expires_after": goldNum(3600),
				"detailed_outputs": goldDetail(map[string]string{"a": ",", "b": futureStamp + ","}),
			})
		},
	},
	{name: "expires_after increase", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "expires_after": goldNum(7200)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{
				"inputs": f.inputs("a"), "expires_after": goldNum(3600),
				"detailed_outputs": goldDetail(map[string]string{"a": ",", "b": futureStamp + ","}),
			})
		},
	},
	{name: "expires_after removed", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value { return map[string]tftypes.Value{"inputs": f.inputs("a")} },
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{
				"inputs": f.inputs("a"), "expires_after": goldNum(3600),
				"detailed_outputs": goldDetail(map[string]string{"a": ",", "b": futureStamp + ","}),
			})
		},
	},
	{name: "delay keeps pending and hides entering", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a", "c"), "delayed_for": goldNum(3600)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{
				"inputs": f.inputs("a", "b"), "delayed_for": goldNum(3600),
				"outputs":          f.coll("b"),
				"detailed_outputs": goldDetail(map[string]string{"a": "," + futureStamp, "b": ","}),
			})
		},
	},
	{name: "delayed_for decrease", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "delayed_for": goldNum(1)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a"}, map[string]tftypes.Value{
				"delayed_for": goldNum(3600), "outputs": f.coll(),
				"detailed_outputs": goldDetail(map[string]string{"a": "," + futureStamp}),
			})
		},
	},
	{name: "delayed_for removed", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value { return map[string]tftypes.Value{"inputs": f.inputs("a")} },
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a"}, map[string]tftypes.Value{
				"delayed_for": goldNum(3600), "outputs": f.coll(),
				"detailed_outputs": goldDetail(map[string]string{"a": "," + futureStamp}),
			})
		},
	},
	{name: "pending leaver", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "delayed_for": goldNum(3600), "expires_after": goldNum(7200)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{
				"delayed_for": goldNum(3600), "expires_after": goldNum(7200), "outputs": f.coll(),
				"detailed_outputs": goldDetail(map[string]string{"a": "," + futureStamp, "b": "," + futureStamp}),
			})
		},
	},
	{name: "reset", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("b"), "triggers_reset": goldStr("v2"), "delayed_for": goldNum(3600)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{"triggers_reset": goldStr("v1")})
		},
	},
	{name: "replacement", apply: true, replace: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("c"), "triggers_replacement": goldStr("r2"), "expires_after": goldNum(3600)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a"}, map[string]tftypes.Value{"triggers_replacement": goldStr("r1")})
		},
	},
	{name: "pre-feature state", apply: true,
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a", "b")}
		},
		state: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "outputs": f.coll("a"), "id": goldStr("state-id")}
		},
	},
	{name: "unknown input",
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.unknownInput()}
		},
		state: func(f goldFlavor) map[string]tftypes.Value { return goldState(f, []string{"a"}, nil) },
	},
	{name: "unknown triggers_reset",
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "triggers_reset": goldUnknown(goldString)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value { return goldState(f, []string{"a"}, nil) },
	},
	{name: "unknown expires_after",
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "expires_after": goldUnknown(tftypes.Number)}
		},
	},
	{name: "unknown delayed_for",
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "delayed_for": goldUnknown(tftypes.Number)}
		},
		state: func(f goldFlavor) map[string]tftypes.Value { return goldState(f, []string{"a"}, nil) },
	},
	{name: "delay not shorter than expiry",
		plan: func(f goldFlavor) map[string]tftypes.Value {
			return map[string]tftypes.Value{"inputs": f.inputs("a"), "delayed_for": goldNum(10), "expires_after": goldNum(10)}
		},
	},
	{name: "corrupt detailed_outputs",
		plan: func(f goldFlavor) map[string]tftypes.Value { return map[string]tftypes.Value{"inputs": f.inputs("a")} },
		state: func(f goldFlavor) map[string]tftypes.Value {
			return goldState(f, []string{"a"}, map[string]tftypes.Value{
				"detailed_outputs": goldDetail(map[string]string{"a": "not-a-time,"}),
			})
		},
	},
}

func runPlanCase(t *testing.T, f goldFlavor, c goldPlanCase) string {
	t.Helper()
	ctx := context.Background()
	f.sch = resourceSchema(t, f.res)
	typ := f.sch.Type().TerraformType(ctx)

	stateRaw := tftypes.NewValue(typ, nil)
	stateID := goldUnknown(goldString)
	if c.state != nil {
		stateRaw = goldObject(ctx, f, c.state(f), false, tftypes.Value{})
		stateID = goldStr("state-id")
	}
	proposed := tftypes.NewValue(typ, nil)
	if c.plan != nil {
		proposed = goldObject(ctx, f, c.plan(f), true, stateID)
	}

	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Raw: proposed, Schema: f.sch},
		Plan:   tfsdk.Plan{Raw: proposed, Schema: f.sch},
		State:  tfsdk.State{Raw: stateRaw, Schema: f.sch},
	}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	f.res.(resource.ResourceWithModifyPlan).ModifyPlan(ctx, req, &resp)

	var b strings.Builder
	fmt.Fprintf(&b, "plan: %s\n", goldRender(resp.Plan.Raw))
	b.WriteString(goldDiags(resp.Diagnostics))
	if !c.apply || resp.Diagnostics.HasError() {
		return b.String()
	}
	for _, path := range []struct {
		label string
		plan  tftypes.Value
	}{{"known", resp.Plan.Raw}, {"fallback", proposed}} {
		fmt.Fprintf(&b, "apply %s: %s", path.label, goldApply(t, ctx, f, path.plan, stateRaw, c))
	}
	return b.String()
}

// goldApply runs Create on create or replacement, otherwise Update.
func goldApply(t *testing.T, ctx context.Context, f goldFlavor, planRaw, stateRaw tftypes.Value, c goldPlanCase) string {
	t.Helper()
	plan := tfsdk.Plan{Raw: planRaw, Schema: f.sch}
	empty := tfsdk.State{Raw: tftypes.NewValue(f.sch.Type().TerraformType(ctx), nil), Schema: f.sch}
	if c.state == nil || c.replace {
		resp := resource.CreateResponse{State: empty}
		f.res.Create(ctx, resource.CreateRequest{Plan: plan, Config: tfsdk.Config{Raw: planRaw, Schema: f.sch}}, &resp)
		return goldRender(resp.State.Raw) + "\n" + goldDiags(resp.Diagnostics)
	}
	state := tfsdk.State{Raw: stateRaw, Schema: f.sch}
	resp := resource.UpdateResponse{State: empty}
	f.res.Update(ctx, resource.UpdateRequest{Plan: plan, State: state, Config: tfsdk.Config{Raw: planRaw, Schema: f.sch}}, &resp)
	return goldRender(resp.State.Raw) + "\n" + goldDiags(resp.Diagnostics)
}

// goldReadCase is a Read scenario over a stored state.
type goldReadCase struct {
	name  string
	state func(f goldFlavor) map[string]tftypes.Value
}

var goldReadCases = []goldReadCase{
	{"nothing due", func(f goldFlavor) map[string]tftypes.Value {
		return goldState(f, []string{"a", "b"}, map[string]tftypes.Value{
			"detailed_outputs": goldDetail(map[string]string{"a": ",", "b": futureStamp + ","}),
		})
	}},
	{"expired culled", func(f goldFlavor) map[string]tftypes.Value {
		return goldState(f, []string{"a", "b", "c"}, map[string]tftypes.Value{
			"detailed_outputs": goldDetail(map[string]string{"a": ",", "b": pastStamp + ",", "c": futureStamp + ","}),
		})
	}},
	{"due revealed, future kept", func(f goldFlavor) map[string]tftypes.Value {
		return goldState(f, []string{"a", "b", "c"}, map[string]tftypes.Value{
			"outputs":          f.coll("a"),
			"detailed_outputs": goldDetail(map[string]string{"a": ",", "b": "," + pastStamp, "c": "," + futureStamp}),
		})
	}},
	{"accumulated null", func(f goldFlavor) map[string]tftypes.Value {
		return map[string]tftypes.Value{"inputs": f.inputs("a"), "outputs": f.coll("a", "b"), "id": goldStr("state-id")}
	}},
	{"corrupt detailed_outputs", func(f goldFlavor) map[string]tftypes.Value {
		return goldState(f, []string{"a"}, map[string]tftypes.Value{
			"detailed_outputs": goldDetail(map[string]string{"a": ",not-a-time"}),
		})
	}},
}

func runReadCase(t *testing.T, f goldFlavor, c goldReadCase) string {
	t.Helper()
	ctx := context.Background()
	f.sch = resourceSchema(t, f.res)
	raw := goldObject(ctx, f, c.state(f), false, tftypes.Value{})
	req := resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: f.sch}}
	resp := resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: f.sch}}
	f.res.Read(ctx, req, &resp)
	return "state: " + goldRender(resp.State.Raw) + "\n" + goldDiags(resp.Diagnostics)
}

type goldImportCase struct{ name, id string }

func goldImportCases(f goldFlavor) []goldImportCase {
	if f.name == "map" {
		return []goldImportCase{
			{"seed", `{"outputs":{"a":"1","b":"2"}}`},
			{"empty", `{"outputs":{}}`},
			{"inputs ignored", `{"inputs":{"z":"9"},"outputs":{"a":"1"}}`},
			{"bad json", `not-json`},
			{"array shape", `{"outputs":["a"]}`},
		}
	}
	return []goldImportCase{
		{"seed", `{"inputs":["a"],"outputs":["a","a","b"]}`},
		{"empty", `{}`},
		{"bad json", `not-json`},
		{"unknown key", `{"outupts":["a"]}`},
		{"wrong value type", `{"outputs":"a"}`},
	}
}

func runImportCase(t *testing.T, f goldFlavor, id string) string {
	t.Helper()
	ctx := context.Background()
	f.sch = resourceSchema(t, f.res)
	resp := resource.ImportStateResponse{State: tfsdk.State{Raw: tftypes.NewValue(f.sch.Type().TerraformType(ctx), nil), Schema: f.sch}}
	f.res.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id}, &resp)
	return "state: " + goldRender(resp.State.Raw) + "\n" + goldDiags(resp.Diagnostics)
}

type goldValidateCase struct {
	name           string
	delay, expires tftypes.Value
}

var goldValidateCases = []goldValidateCase{
	{"equal", goldNum(10), goldNum(10)},
	{"greater", goldNum(11), goldNum(10)},
	{"shorter", goldNum(9), goldNum(10)},
	{"unknown", goldUnknown(tftypes.Number), goldNum(10)},
}

func runValidateCase(t *testing.T, f goldFlavor, c goldValidateCase) string {
	t.Helper()
	ctx := context.Background()
	f.sch = resourceSchema(t, f.res)
	raw := goldObject(ctx, f, map[string]tftypes.Value{
		"inputs": f.inputs("a"), "delayed_for": c.delay, "expires_after": c.expires,
	}, false, tftypes.Value{})
	resp := resource.ValidateConfigResponse{}
	f.res.(resource.ResourceWithValidateConfig).ValidateConfig(ctx,
		resource.ValidateConfigRequest{Config: tfsdk.Config{Raw: raw, Schema: f.sch}}, &resp)
	return goldDiags(resp.Diagnostics)
}

var goldTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

// goldRender writes a value deterministically: object and map keys sorted,
// list and set elements in stored order, unknown as <unknown>, and
// clock-derived timestamps as <clock>.
func goldRender(v tftypes.Value) string {
	if !v.IsKnown() {
		return "<unknown>"
	}
	if v.IsNull() {
		return "null"
	}
	switch {
	case v.Type().Is(tftypes.String):
		var s string
		_ = v.As(&s)
		if goldTimestamp.MatchString(s) && s != pastStamp && s != futureStamp {
			return "<clock>"
		}
		return fmt.Sprintf("%q", s)
	case v.Type().Is(tftypes.Number):
		var n big.Float
		_ = v.As(&n)
		return n.Text('g', -1)
	case v.Type().Is(tftypes.List{}), v.Type().Is(tftypes.Set{}):
		var elems []tftypes.Value
		_ = v.As(&elems)
		parts := make([]string, 0, len(elems))
		for _, e := range elems {
			parts = append(parts, goldRender(e))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case v.Type().Is(tftypes.Map{}), v.Type().Is(tftypes.Object{}):
		var m map[string]tftypes.Value
		_ = v.As(&m)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%q: %s", k, goldRender(m[k])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return v.String()
}

// goldDiags writes each diagnostic's severity, attribute path, summary, and
// detail, one per line.
func goldDiags(diags diag.Diagnostics) string {
	var b strings.Builder
	for _, d := range diags {
		path := ""
		if dp, ok := d.(diag.DiagnosticWithPath); ok {
			path = dp.Path().String()
		}
		fmt.Fprintf(&b, "diag %s [%s] %s | %s\n", d.Severity(), path, d.Summary(), d.Detail())
	}
	return b.String()
}
