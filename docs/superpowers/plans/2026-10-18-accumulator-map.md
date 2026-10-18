# accumulator_map Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to execute this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `accumulator_map` resource whose `inputs` and `outputs` are `map(string)`, keyed by map key, with the same reset, replacement, expiry, delay, import, and id conventions as `accumulator_set`.

**Architecture:** Keys are the identity. New pure functions in `internal/accumulate/map.go` merge maps (planned value overwrites stored value), and every existing stamp function (`FilterExpired`, `FilterPending`, `ClassifyStamps`, `ClassifyDelays`, `Visible`, `ApplyExpiration`, `ApplyDelay`) and provider composition (`planStamps`, `applyStamps`, `applyFallback`) runs unchanged over the sorted key slice. `internal/provider/resource_map.go` mirrors `resource_set.go` and projects key slices back onto the merged map at the end of each phase.

**Tech Stack:** Go 1.25, terraform-plugin-framework v1.19, terraform-plugin-testing v1.16, OpenTofu >= 1.10 for acceptance tests, tfplugindocs via `make docs` (needs `tofu` >= 1.11 on PATH).

**Spec:** `docs/superpowers/specs/2026-10-18-accumulator-map-design.md`

## Global Constraints

- Every Go file starts with `// SPDX-License-Identifier: MPL-2.0` (house test `TestEveryGoFileHasTheSPDXHeader`).
- No `--` inside any string literal in non-test files of `internal/provider`; write a real em dash `—` (house test `TestUserFacingStringsUseEmDashes`).
- Collections written to state are never null: an empty map is `{}`, an empty slice is `[]`.
- ModifyPlan reads no clock. Only `Read`, `Create`, and `Update` call `clockNow()`.
- Values are `string`; the resource type name is `accumulator_map`; Go names are `mapResource`, `NewMapResource`, `mapResourceModel`.
- Run unit tests with `go test ./internal/...`; run acceptance tests with `make testacc TESTARGS='-run TestAccMap'` (needs `tofu` on PATH). Run `gofmt -l .` and `go vet ./...` before each commit.
- Commit messages follow the repository's Conventional Commits style (`feat:`, `test:`, `docs:`), imperative, no trailing period. Stage explicit paths only.

## Review Focus

Inputs the spec implies but names no test for, each pinned to a task below:

1. `inputs = {}` on create: `accumulated` and `outputs` are `{}` and `id` is `sha256("{}")`, not null and not an error. Pinned in Task 6.
2. Keys containing `.` or `/` (such as `svc/web` and `a.b`) round-trip through `outputs` and `detailed_outputs` without splitting into nested paths. Pinned in Task 6.
3. An empty-string value `""` is a real value: it merges, overwrites, and survives in `outputs`. Pinned in Tasks 1 and 6.
4. A value-only change with `expires_after` set plans fully known with no unknown stamp, because no key enters or leaves. Pinned in Task 7.
5. Importing `{"outputs":{}}` seeds empty maps and the hash of `{}`, and the first apply merges the configuration. Pinned in Task 9.

---

### Task 1: Pure map functions

**Files:**
- Create: `internal/accumulate/map.go`
- Test: `internal/accumulate/map_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func MergeMap(outputs, inputs map[string]string) map[string]string`
  - `func NextMapOutputs(reseed bool, planInputs, stateAccumulated map[string]string) map[string]string`
  - `func SortedKeys(m map[string]string) []string`
  - `func Project(m map[string]string, keys []string) map[string]string`

- [ ] **Step 1: Write the failing tests**

```go
// SPDX-License-Identifier: MPL-2.0

package accumulate

import (
	"reflect"
	"testing"
)

func TestMergeMap(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		outputs, inputs map[string]string
		want            map[string]string
	}{
		"nil and nil":          {nil, nil, map[string]string{}},
		"nil and empty":        {nil, map[string]string{}, map[string]string{}},
		"new key added":        {map[string]string{"a": "1"}, map[string]string{"b": "2"}, map[string]string{"a": "1", "b": "2"}},
		"existing overwritten": {map[string]string{"a": "1"}, map[string]string{"a": "2"}, map[string]string{"a": "2"}},
		"same value no change": {map[string]string{"a": "1"}, map[string]string{"a": "1"}, map[string]string{"a": "1"}},
		"empty string value":   {map[string]string{"a": "1"}, map[string]string{"a": ""}, map[string]string{"a": ""}},
		"empty inputs":         {map[string]string{"a": "1"}, map[string]string{}, map[string]string{"a": "1"}},
		"empty outputs":        {map[string]string{}, map[string]string{"a": "1"}, map[string]string{"a": "1"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := MergeMap(tc.outputs, tc.inputs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MergeMap(%v, %v) = %v, want %v", tc.outputs, tc.inputs, got, tc.want)
			}
			if got == nil {
				t.Fatal("MergeMap returned nil; it must always return a non-nil map")
			}
		})
	}
}

// TestMergeMapDoesNotMutate pins that callers can keep using the maps they
// passed in: state-derived maps must not change under the resource.
func TestMergeMapDoesNotMutate(t *testing.T) {
	t.Parallel()
	outputs := map[string]string{"a": "1"}
	inputs := map[string]string{"a": "2", "b": "3"}
	got := MergeMap(outputs, inputs)
	got["c"] = "4"
	if !reflect.DeepEqual(outputs, map[string]string{"a": "1"}) {
		t.Fatalf("MergeMap mutated outputs: %v", outputs)
	}
	if !reflect.DeepEqual(inputs, map[string]string{"a": "2", "b": "3"}) {
		t.Fatalf("MergeMap mutated inputs: %v", inputs)
	}
}

func TestNextMapOutputs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		reseed           bool
		planInputs       map[string]string
		stateAccumulated map[string]string
		want             map[string]string
	}{
		"reseed keeps only the planned inputs": {true, map[string]string{"d": "1"}, map[string]string{"a": "1"}, map[string]string{"d": "1"}},
		"merge adds the planned inputs":        {false, map[string]string{"d": "1"}, map[string]string{"a": "1"}, map[string]string{"a": "1", "d": "1"}},
		"merge overwrites a shared key":        {false, map[string]string{"a": "2"}, map[string]string{"a": "1"}, map[string]string{"a": "2"}},
		"merge onto nil history":               {false, map[string]string{"d": "1"}, nil, map[string]string{"d": "1"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := NextMapOutputs(tc.reseed, tc.planInputs, tc.stateAccumulated)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("NextMapOutputs(%v, %v, %v) = %v, want %v", tc.reseed, tc.planInputs, tc.stateAccumulated, got, tc.want)
			}
			if got == nil {
				t.Fatal("NextMapOutputs returned nil; it must always return a non-nil map")
			}
		})
	}
}

func TestSortedKeys(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		m    map[string]string
		want []string
	}{
		"nil":      {nil, []string{}},
		"empty":    {map[string]string{}, []string{}},
		"ordered":  {map[string]string{"b": "1", "a": "1", "c": "1"}, []string{"a", "b", "c"}},
		"one":      {map[string]string{"z": ""}, []string{"z"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := SortedKeys(tc.m)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SortedKeys(%v) = %v, want %v", tc.m, got, tc.want)
			}
			if got == nil {
				t.Fatal("SortedKeys returned nil; it must always return a non-nil slice")
			}
		})
	}
}

func TestProject(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		m    map[string]string
		keys []string
		want map[string]string
	}{
		"nil map":          {nil, []string{"a"}, map[string]string{}},
		"nil keys":         {map[string]string{"a": "1"}, nil, map[string]string{}},
		"subset":           {map[string]string{"a": "1", "b": "2"}, []string{"b"}, map[string]string{"b": "2"}},
		"all":              {map[string]string{"a": "1", "b": "2"}, []string{"a", "b"}, map[string]string{"a": "1", "b": "2"}},
		"missing skipped":  {map[string]string{"a": "1"}, []string{"a", "zz"}, map[string]string{"a": "1"}},
		"empty value kept": {map[string]string{"a": ""}, []string{"a"}, map[string]string{"a": ""}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Project(tc.m, tc.keys)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Project(%v, %v) = %v, want %v", tc.m, tc.keys, got, tc.want)
			}
			if got == nil {
				t.Fatal("Project returned nil; it must always return a non-nil map")
			}
		})
	}
}

// TestProjectDoesNotAlias pins that the projection is a new map.
func TestProjectDoesNotAlias(t *testing.T) {
	t.Parallel()
	m := map[string]string{"a": "1"}
	got := Project(m, []string{"a"})
	got["a"] = "changed"
	if m["a"] != "1" {
		t.Fatalf("Project aliased its input: %v", m)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/accumulate/ -run 'TestMergeMap|TestNextMapOutputs|TestSortedKeys|TestProject' -v`
Expected: build failure, `undefined: MergeMap` (and the other three names).

- [ ] **Step 3: Write the implementation**

```go
// SPDX-License-Identifier: MPL-2.0

package accumulate

import "sort"

// MergeMap returns a copy of outputs with every entry of inputs written
// over it: a new key is added and an existing key takes the input value
// (spec M1, overwrite in place). Neither argument is mutated. The result is
// never nil, so the value written to state is a concrete map.
//
// It is idempotent: applying it whenever the resource is updated is correct
// whether or not the inputs changed, which removes the need for the resource
// to compare planned inputs against state.
func MergeMap(outputs, inputs map[string]string) map[string]string {
	merged := make(map[string]string, len(outputs)+len(inputs))
	for k, v := range outputs {
		merged[k] = v
	}
	for k, v := range inputs {
		merged[k] = v
	}
	return merged
}

// NextMapOutputs returns the accumulated map accumulator_map writes for the
// next apply, whether that apply is planned (ModifyPlan) or performed
// (Update). It is the whole branch decision: reseed (a replacement or a
// triggers_reset change) discards history and seeds from the planned inputs
// alone (spec M2); otherwise the planned inputs are merged over the prior
// map (spec M1).
func NextMapOutputs(reseed bool, planInputs, stateAccumulated map[string]string) map[string]string {
	if reseed {
		return MergeMap(nil, planInputs)
	}
	return MergeMap(stateAccumulated, planInputs)
}

// SortedKeys returns the keys of m in ascending order. A nil or empty m
// yields an empty (non-nil) slice. Every call from the map resource into the
// stamp functions passes this, so plan and apply classify the same keys in
// the same order and detailed_outputs is deterministic.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Project returns the entries of m whose keys are in keys, as a new map. It
// turns a key list returned by a stamp function (survivors, visible) back
// into the map(string) the resource writes. A key absent from m is skipped.
// The result is never nil and never aliases m.
func Project(m map[string]string, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/accumulate/ -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./internal/accumulate/
git add internal/accumulate/map.go internal/accumulate/map_test.go
git commit -m "feat: add pure map merge and projection functions"
```

---

### Task 2: Map hashing

**Files:**
- Edit: `internal/accumulate/id.go` (append after `HashID`)
- Test: `internal/accumulate/id_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces: `func HashMapID(values map[string]string) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/accumulate/id_test.go`:

```go
func TestHashMapID(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values map[string]string
		want   string
	}{
		"nil normalizes to an empty object": {
			nil,
			"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", // sha256("{}")
		},
		"empty map": {
			map[string]string{},
			"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", // sha256("{}")
		},
		"one entry": {
			map[string]string{"a": "1"},
			"9afeb0f2b203f254312ec8ded441d0318b7c34c57f8695ede42d2215a30c0960", // sha256(`{"a":"1"}`)
		},
		"two entries": {
			map[string]string{"b": "1", "a": "1"},
			"40239544dc03c3ec3a09cb947c72123697428a72af96b1c63431f34a46517709", // sha256(`{"a":"1","b":"1"}`)
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			if got := HashMapID(tc.values); got != tc.want {
				t.Fatalf("HashMapID(%v) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

// TestHashMapIDDiffersFromKeyList pins that a map hashes its JSON object,
// not its key list: {"a":"1"} and ["a"] must not collide.
func TestHashMapIDDiffersFromKeyList(t *testing.T) {
	t.Parallel()
	if HashMapID(map[string]string{"a": "1"}) == HashID([]string{"a"}) {
		t.Fatal("HashMapID collides with HashID of the key list")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/accumulate/ -run 'TestHashMapID' -v`
Expected: build failure, `undefined: HashMapID`.

- [ ] **Step 3: Write the implementation**

Append to `internal/accumulate/id.go`:

```go
// HashMapID is HashID for a map: the lowercase hex SHA-256 of the canonical
// JSON object encoding of values. encoding/json sorts object keys, so two
// maps with the same entries hash the same regardless of insertion order. A
// nil map is normalized to an empty one first, so it hashes "{}" rather than
// JSON null.
func HashMapID(values map[string]string) string {
	if values == nil {
		values = map[string]string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		// json.Marshal of a map[string]string has no error path. Keeping the
		// branch means a future change to the value type cannot silently hash
		// empty bytes.
		panic(fmt.Sprintf("accumulate: hashing %T: %v", values, err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/accumulate/ -run 'TestHash' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./internal/accumulate/
git add internal/accumulate/id.go internal/accumulate/id_test.go
git commit -m "feat: hash map resources by canonical JSON object"
```

---

### Task 3: Map import parsing

**Files:**
- Edit: `internal/accumulate/import.go` (append after `stringArray`)
- Test: `internal/accumulate/import_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type MapImportSeed struct { Inputs, Outputs map[string]string }`
  - `func ParseMapImport(id string) (MapImportSeed, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/accumulate/import_test.go`:

```go
func TestParseMapImport(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		id   string
		want MapImportSeed
	}{
		"both keys": {
			`{"inputs":{"a":"1"},"outputs":{"a":"1","b":"2"}}`,
			MapImportSeed{Inputs: map[string]string{"a": "1"}, Outputs: map[string]string{"a": "1", "b": "2"}},
		},
		"outputs only": {
			`{"outputs":{"a":"1"}}`,
			MapImportSeed{Inputs: map[string]string{}, Outputs: map[string]string{"a": "1"}},
		},
		"inputs only": {
			`{"inputs":{"a":"1"}}`,
			MapImportSeed{Inputs: map[string]string{"a": "1"}, Outputs: map[string]string{}},
		},
		"empty object": {
			`{}`,
			MapImportSeed{Inputs: map[string]string{}, Outputs: map[string]string{}},
		},
		"empty objects stay empty": {
			`{"inputs":{},"outputs":{}}`,
			MapImportSeed{Inputs: map[string]string{}, Outputs: map[string]string{}},
		},
		"empty string value": {
			`{"outputs":{"a":""}}`,
			MapImportSeed{Inputs: map[string]string{}, Outputs: map[string]string{"a": ""}},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMapImport(tc.id)
			if err != nil {
				t.Fatalf("ParseMapImport(%q): %v", tc.id, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseMapImport(%q) = %+v, want %+v", tc.id, got, tc.want)
			}
			if got.Inputs == nil || got.Outputs == nil {
				t.Fatal("ParseMapImport returned a nil map; a missing key must mean empty, not null")
			}
		})
	}
}

func TestParseMapImportRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		id          string
		wantContain string
		wantAbsent  string
	}{
		"not json":          {`not-json`, "must be a JSON object", ""},
		"json array":        {`[]`, "must be a JSON object", ""},
		"json null":         {`null`, "must be a JSON object", ""},
		"unknown key":       {`{"outupts":{"a":"1"}}`, `"outupts" is not recognized`, ""},
		"array value":       {`{"outputs":["a"]}`, `"outputs" must be an object of strings`, ""},
		"null value":        {`{"outputs":null}`, `"outputs" must be an object of strings`, ""},
		"number entry":      {`{"outputs":{"a":1}}`, `"outputs" must be an object of strings`, "1"},
		"nested object":     {`{"outputs":{"a":{"b":"c"}}}`, `"outputs" must be an object of strings`, "c"},
		"string instead":    {`{"inputs":"a"}`, `"inputs" must be an object of strings`, ""},
		"first bad key wins": {`{"zzz":{},"aaa":{}}`, `"aaa" is not recognized`, "zzz"},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			_, err := ParseMapImport(tc.id)
			if err == nil {
				t.Fatalf("ParseMapImport(%q) accepted a malformed ID", tc.id)
			}
			if !strings.Contains(err.Error(), tc.wantContain) {
				t.Fatalf("ParseMapImport(%q) error = %q, want it to contain %q", tc.id, err, tc.wantContain)
			}
			if tc.wantAbsent != "" && strings.Contains(err.Error(), tc.wantAbsent) {
				t.Fatalf("ParseMapImport(%q) error = %q leaks payload %q; errors quote only the key", tc.id, err, tc.wantAbsent)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/accumulate/ -run 'TestParseMapImport' -v`
Expected: build failure, `undefined: MapImportSeed`.

- [ ] **Step 3: Write the implementation**

Append to `internal/accumulate/import.go`:

```go
// MapImportSeed is the parsed form of an accumulator_map import ID. A
// missing key is an empty map, never nil, so the caller always writes a
// concrete map to state.
type MapImportSeed struct {
	Inputs  map[string]string
	Outputs map[string]string
}

// ParseMapImport parses an accumulator_map import ID, which is a JSON
// object whose values are objects of strings:
//
//	{"inputs":{"k":"v"},"outputs":{"k":"v","k2":"v2"}}
//
// The same contract as ParseImport: only "inputs" and "outputs" are
// recognized, each must be a JSON object whose values are strings, and
// errors name the expected shape and quote the offending key only.
func ParseMapImport(id string) (MapImportSeed, error) {
	seed := MapImportSeed{Inputs: map[string]string{}, Outputs: map[string]string{}}

	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(id), &object); err != nil || object == nil {
		return MapImportSeed{}, fmt.Errorf(
			"import ID must be a JSON object with optional \"inputs\" and \"outputs\" objects of strings, " +
				"for example {\"inputs\":{\"k\":\"v\"},\"outputs\":{\"k\":\"v\"}}")
	}

	keys := make([]string, 0, len(object))
	for k := range object {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if k != "inputs" && k != "outputs" {
			return MapImportSeed{}, fmt.Errorf(
				"import ID key %q is not recognized; the only recognized keys are \"inputs\" and \"outputs\"", k)
		}
		values, err := stringObject(object[k])
		if err != nil {
			return MapImportSeed{}, fmt.Errorf("import ID key %q %v", k, err)
		}
		if k == "inputs" {
			seed.Inputs = values
		} else {
			seed.Outputs = values
		}
	}
	return seed, nil
}

// stringObject decodes a JSON value that must be an object of strings. A
// JSON null decodes into a nil map with no error, so it is rejected
// explicitly, as are arrays, numbers, nested objects, and booleans inside
// the object.
func stringObject(raw json.RawMessage) (map[string]string, error) {
	var items map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, fmt.Errorf("must be an object of strings")
	}
	out := make(map[string]string, len(items))
	for k, item := range items {
		var s string
		if err := json.Unmarshal(item, &s); err != nil {
			return nil, fmt.Errorf("must be an object of strings")
		}
		out[k] = s
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/accumulate/ -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./internal/accumulate/
git add internal/accumulate/import.go internal/accumulate/import_test.go
git commit -m "feat: parse map-shaped import IDs"
```

---

### Task 4: Provider map conversion helpers

**Files:**
- Edit: `internal/provider/model.go` (append after `setFromStrings`, before `detailedObjectType`)
- Test: `internal/provider/model_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func stringMapFromMap(ctx context.Context, m types.Map) (map[string]string, diag.Diagnostics)`
  - `func mapFromStringMap(ctx context.Context, values map[string]string) (types.Map, diag.Diagnostics)`
  - `func mapContainsUnknown(m types.Map) bool`

- [ ] **Step 1: Write the failing tests**

Append to `internal/provider/model_test.go`:

```go
func TestMapFromStringMapIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string]map[string]string{
		"nil":    nil,
		"empty":  {},
		"values": {"a": "1", "b": ""},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := mapFromStringMap(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("mapFromStringMap: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("mapFromStringMap produced a null map; state must always hold a concrete value")
			}
			if got.IsUnknown() {
				t.Fatal("mapFromStringMap produced an unknown map")
			}
			if got.ElementType(context.Background()) != types.StringType {
				t.Fatalf("element type = %v, want string", got.ElementType(context.Background()))
			}
		})
	}
}

func TestStringMapFromMapRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	want := map[string]string{"a": "1", "b": ""}
	m, diags := mapFromStringMap(ctx, want)
	if diags.HasError() {
		t.Fatalf("mapFromStringMap: %v", diags.Errors())
	}
	got, diags := stringMapFromMap(ctx, m)
	if diags.HasError() {
		t.Fatalf("stringMapFromMap: %v", diags.Errors())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}

func TestStringMapFromMapNullAndUnknownAreNil(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for label, m := range map[string]types.Map{
		"null":    types.MapNull(types.StringType),
		"unknown": types.MapUnknown(types.StringType),
	} {
		m := m
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := stringMapFromMap(ctx, m)
			if diags.HasError() {
				t.Fatalf("stringMapFromMap: %v", diags.Errors())
			}
			if got != nil {
				t.Fatalf("stringMapFromMap(%s) = %v, want nil", label, got)
			}
		})
	}
}

func TestMapContainsUnknown(t *testing.T) {
	t.Parallel()
	known := types.MapValueMust(types.StringType, map[string]attr.Value{"a": types.StringValue("1")})
	partial := types.MapValueMust(types.StringType, map[string]attr.Value{
		"a": types.StringValue("1"), "b": types.StringUnknown(),
	})
	for label, tc := range map[string]struct {
		m    types.Map
		want bool
	}{
		"known":          {known, false},
		"null":           {types.MapNull(types.StringType), false},
		"whole unknown":  {types.MapUnknown(types.StringType), true},
		"element unknown": {partial, true},
	} {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			if got := mapContainsUnknown(tc.m); got != tc.want {
				t.Fatalf("mapContainsUnknown = %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run 'TestMapFromStringMap|TestStringMapFromMap|TestMapContainsUnknown' -v`
Expected: build failure, `undefined: mapFromStringMap`.

- [ ] **Step 3: Write the implementation**

Insert into `internal/provider/model.go` after `setFromStrings`:

```go
// stringMapFromMap converts a map(string) to a Go map. Null and unknown are
// both treated as absent and produce a nil map with no diagnostic, matching
// stringsFromList: a null required attribute is a schema violation the
// framework already reports, and an unknown value is resolved before Create
// or Update runs.
func stringMapFromMap(ctx context.Context, m types.Map) (map[string]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m.IsNull() || m.IsUnknown() {
		return nil, diags
	}
	var out map[string]string
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// mapFromStringMap converts a Go map to a concrete map(string). A nil map
// becomes an empty map, never null: an empty accumulated history must be
// written as {} so state round-trips without a diff against an empty config.
func mapFromStringMap(ctx context.Context, values map[string]string) (types.Map, diag.Diagnostics) {
	if values == nil {
		values = map[string]string{}
	}
	return types.MapValueFrom(ctx, types.StringType, values)
}

// mapContainsUnknown is listContainsUnknown for a map(string): the map
// itself or any of its values unknown. A map whose keys are unknown is
// wholly unknown in Terraform, so the element walk only ever meets unknown
// values.
func mapContainsUnknown(m types.Map) bool {
	if m.IsUnknown() {
		return true
	}
	for _, element := range m.Elements() {
		if element.IsUnknown() {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/provider/ -run 'TestMapFromStringMap|TestStringMapFromMap|TestMapContainsUnknown' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./internal/provider/
git add internal/provider/model.go internal/provider/model_test.go
git commit -m "feat: add map(string) conversion helpers"
```

---

### Task 5: The accumulator_map resource

**Files:**
- Create: `internal/provider/resource_map.go`
- Edit: `internal/provider/provider.go:55-60` (register the resource)
- Edit: `internal/provider/provider_test.go:97` (the resource-name loop in `TestProviderSchema`)
- Test: `internal/provider/read_test.go` (append)

**Interfaces:**
- Consumes: `accumulate.MergeMap`, `accumulate.NextMapOutputs`, `accumulate.SortedKeys`, `accumulate.Project`, `accumulate.HashMapID`, `accumulate.ParseMapImport` (Tasks 1 to 3); `stringMapFromMap`, `mapFromStringMap`, `mapContainsUnknown` (Task 4); existing `stampArgs`, `planStamps`, `applyStamps`, `applyFallback`, `stampsFromMap`, `mapFromStamps`, `checkDelayShorterThanExpiry`, `clockNow`, `int64Ptr`, `detailedObjectType`, `maxExpiresAfter`.
- Produces: `NewMapResource() resource.Resource`, `mapResource`, `mapResourceModel`, and the registered `accumulator_map` type.

- [ ] **Step 1: Write the failing schema test**

In `internal/provider/provider_test.go`, change the loop header in `TestProviderSchema` from

```go
	for _, name := range []string{"accumulator_list", "accumulator_set"} {
```

to

```go
	for _, name := range []string{"accumulator_list", "accumulator_set", "accumulator_map"} {
```

and change the `accumulated` expectation block from

```go
		accumulated, ok := attrs["accumulated"]
		wantAccumulated := tftypes.Type(tftypes.Set{ElementType: tftypes.String})
		if name == "accumulator_list" {
			wantAccumulated = tftypes.List{ElementType: tftypes.String}
		}
```

to

```go
		accumulated, ok := attrs["accumulated"]
		wantAccumulated := tftypes.Type(tftypes.Set{ElementType: tftypes.String})
		switch name {
		case "accumulator_list":
			wantAccumulated = tftypes.List{ElementType: tftypes.String}
		case "accumulator_map":
			wantAccumulated = tftypes.Map{ElementType: tftypes.String}
		}
```

Then append, inside the same loop after the `accumulated` switch statement:

```go
		outputs, ok := attrs["outputs"]
		switch {
		case !ok:
			t.Errorf("%s: missing outputs attribute", name)
		case !outputs.Computed || outputs.Optional || outputs.Required:
			t.Errorf("%s: outputs must be Computed only, got %+v", name, outputs)
		case !outputs.Type.Is(wantAccumulated):
			t.Errorf("%s: outputs type = %s, want %s", name, outputs.Type, wantAccumulated)
		}
		if name == "accumulator_map" {
			inputs, ok := attrs["inputs"]
			switch {
			case !ok:
				t.Errorf("%s: missing inputs attribute", name)
			case !inputs.Required || inputs.Optional || inputs.Computed:
				t.Errorf("%s: inputs must be Required only, got %+v", name, inputs)
			case !inputs.Type.Is(tftypes.Map{ElementType: tftypes.String}):
				t.Errorf("%s: inputs type = %s, want map(string)", name, inputs.Type)
			}
		}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/provider/ -run TestProviderSchema -v`
Expected: FAIL with `provider did not register resource "accumulator_map"`.

- [ ] **Step 3: Write the resource**

Create `internal/provider/resource_map.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

var (
	_ resource.Resource                   = (*mapResource)(nil)
	_ resource.ResourceWithImportState    = (*mapResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*mapResource)(nil)
	_ resource.ResourceWithValidateConfig = (*mapResource)(nil)
)

// mapResource accumulates inputs across applies into a map keyed by the
// input keys. The key is the identity: every membership, expiry, and delay
// decision is made per key, and a re-supplied key takes the planned value
// (spec section 3, overwrite in place). State is the only store, so Delete
// is a no-op and Read only ever removes expired keys and reveals due ones.
type mapResource struct{}

// NewMapResource returns the accumulator_map resource.
func NewMapResource() resource.Resource {
	return &mapResource{}
}

// priorAccumulated reads the store from state: accumulated, or outputs when
// state lacks it (spec section 10).
func (m mapResourceModel) priorAccumulated(ctx context.Context) (map[string]string, diag.Diagnostics) {
	if m.Accumulated.IsNull() {
		return stringMapFromMap(ctx, m.Outputs)
	}
	return stringMapFromMap(ctx, m.Accumulated)
}

// ValidateConfig is spec C1 on raw configuration: delayed_for must be
// strictly less than expires_after when both are known. Unknown values skip
// the check here; ModifyPlan repeats it once they resolve.
func (r *mapResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config mapResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(config.DelayedFor, config.ExpiresAfter)...)
}

// mapResourceModel is accumulator_map's state model.
type mapResourceModel struct {
	Inputs              types.Map    `tfsdk:"inputs"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	DelayedFor          types.Int64  `tfsdk:"delayed_for"`
	Accumulated         types.Map    `tfsdk:"accumulated"`
	Outputs             types.Map    `tfsdk:"outputs"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
	ID                  types.String `tfsdk:"id"`
}

func (r *mapResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_map"
}

func (r *mapResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies into an `outputs` map keyed by the input " +
			"keys. Every key ever seen is retained with its most recent value: re-supplying a key with " +
			"a different value overwrites the stored value in place, and the key keeps its identity. A " +
			"key is forgotten by a `triggers_reset` change, a replacement, or — when `expires_after` is " +
			"set — expiry after the TTL; see `expires_after` for the clock rules. With `delayed_for` " +
			"set, a newly supplied key lands in `accumulated` at once but joins `outputs` only after " +
			"the delay; see `delayed_for` for the clock rules. A value change never delays or expires " +
			"a key; only key presence matters.",
		Attributes: map[string]schema.Attribute{
			"inputs": schema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The entries to accumulate. Terraform stores `inputs` in state so " +
					"the value round-trips; an update merges it into `outputs` (or, when " +
					"`triggers_reset` changes, reseeds `outputs` from it). A key already present takes " +
					"the supplied value; re-supplying a key with the same value changes nothing.",
			},
			"triggers_reset": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Change this value to discard the accumulated map and reseed " +
					"`outputs` from `inputs`, forgetting every key seen before the change. Any change " +
					"counts, including from null to a value. Leave it null for no reset.",
			},
			"triggers_replacement": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				MarkdownDescription: "Change this value to force a replacement (destroy and recreate), " +
					"which reseeds `outputs` from `inputs`. Any change counts, including from null to a " +
					"value. Leave it null for no replacement.",
			},
			"expires_after": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, maxExpiresAfter)},
				MarkdownDescription: "How many seconds a key that has left `inputs` survives in " +
					"`outputs`. While a key stays in `inputs` it never expires and its `expires_at` " +
					"is null, whatever happens to its value. Expiration is realized at refresh: a key is " +
					"removed when a refresh runs after its `expires_at` has passed, and running with " +
					"`-refresh=false` defers removal until the next refresh. A late-applied saved plan " +
					"will not expire an entry even if the time has passed by the time the plan is " +
					"applied — its refresh already ran at plan time; a new plan/apply is required. The " +
					"entry for a key that leaves `inputs` (or whose stamp is being recalculated after an " +
					"`expires_after` change) shows as known after apply in that apply's plan. Changing " +
					"the value recalculates every existing `expires_at` as min(old, new) on a decrease " +
					"and max(old, new) on an increase. Must be between 0 and 9223372036 (about 292 " +
					"years); `0` stamps a key with the current moment as it leaves `inputs`, so the next " +
					"refresh removes it. Leave it null to disable expiration entirely.",
			},
			"delayed_for": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, maxExpiresAfter)},
				MarkdownDescription: "How many seconds a key waits in `accumulated` before it appears in " +
					"`outputs`. The apply that first accumulates a key stamps its `visible_at` as now " +
					"plus `delayed_for` and leaves it out of `outputs`; the reveal is realized at " +
					"refresh: a key joins `outputs` when a refresh runs after its `visible_at` has " +
					"passed, and running with `-refresh=false` defers the reveal until the next refresh. " +
					"A late-applied saved plan will not reveal an entry even if the time has passed by " +
					"the time the plan is applied — its refresh already ran at plan time; a new " +
					"plan/apply is required. Create, replacement, and a `triggers_reset` change delay " +
					"every seeded key. A value change on a key that is already present never delays it. " +
					"Setting `delayed_for` on an existing resource hides nothing that is already visible. " +
					"Changing the value recalculates every pending `visible_at` as min(old, new) on a " +
					"decrease and max(old, new) on an increase; removing it reveals every pending key at " +
					"that apply. Must be between 0 and 9223372036 (about 292 years) and strictly less " +
					"than `expires_after` when both are set; `0` stamps the current moment so the next " +
					"refresh reveals the key. Leave it null to disable the delay entirely.",
			},
			"accumulated": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Every key ever accumulated with its most recent value, including " +
					"keys still waiting out `delayed_for`. `outputs` is this map minus the pending keys; " +
					"with `delayed_for` null the two are always equal. Computed; never configured. Known " +
					"at plan time under the same conditions as `outputs`.",
			},
			"detailed_outputs": schema.MapAttribute{
				Computed:    true,
				ElementType: detailedObjectType,
				MarkdownDescription: "A map from every key in `accumulated` to its timing attributes. " +
					"`expires_at` is an RFC 3339 timestamp of when the key will be removed, or null " +
					"when the key cannot expire: it is in `inputs`, or `expires_after` is null. " +
					"`visible_at` is an RFC 3339 timestamp of when a pending key will join `outputs`, " +
					"or null when it is already visible. Computed; never configured. Entries being " +
					"stamped by an apply show as known after apply in that apply's plan. Expiry and " +
					"reveal are realized at refresh; a late-applied saved plan will not expire or " +
					"reveal an entry whose time passed after the plan was created — a new plan/apply is " +
					"required.",
			},
			"outputs": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The visible entries: `accumulated` minus every key still waiting " +
					"out `delayed_for`, values carried verbatim. Computed; never configured. The plan " +
					"shows the value the next apply will produce, computed against the prior state. " +
					"When `inputs` (including any single value), a trigger, `expires_after`, or " +
					"`delayed_for` is itself unknown at plan time, `outputs` plans as unknown and the " +
					"apply resolves it.",
			},
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				MarkdownDescription: "A stable identifier: the lowercase hex SHA-256 of the canonical " +
					"JSON encoding of the resource's initial values. On create (and replacement) that " +
					"is the `inputs` map; on import it is the seeded `outputs`, because a map resource " +
					"seeds no meaningful `inputs`. Stable across in-place updates.",
			},
		},
	}
}

// Create seeds accumulated from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
//
// Known path: ModifyPlan already computed accumulated, outputs,
// detailed_outputs, and the id without reading the clock; membership is
// realized verbatim and only the fresh visible_at entries are resolved
// with the apply clock. Fallback path (unknown branch inputs at plan time):
// create is the reseed case, every key comes from inputs, and both stamps
// come from the apply clock through applyFallback.
func (r *mapResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	args := stampArgs{
		PlanInputs:     accumulate.SortedKeys(inputs),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		accumulated, d := stringMapFromMap(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulate.SortedKeys(accumulated), args, clockNow())
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextMapOutputs(true, inputs, nil)
	survivors, visible, detailed, d := applyFallback(ctx, accumulate.SortedKeys(merged), args, clockNow())
	resp.Diagnostics.Append(d...)
	acc, d := mapFromStringMap(ctx, accumulate.Project(merged, survivors))
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(merged, visible))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashMapID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan writes the outputs the next apply will produce into the plan.
// It runs the same accumulation algorithm as Create and Update, through the
// same accumulate.NextMapOutputs call, so the planned value cannot disagree
// with the applied one. It reads no clock: fresh detailed_outputs entries
// plan as unknown and the apply stamps them. When any value the branch
// decision depends on is still unknown, outputs is left unknown and the
// apply resolves it.
func (r *mapResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	creating := req.State.Raw.IsNull()

	var state mapResourceModel
	if !creating {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if mapContainsUnknown(plan.Inputs) || plan.TriggersReset.IsUnknown() ||
		plan.TriggersReplacement.IsUnknown() || plan.ExpiresAfter.IsUnknown() ||
		plan.DelayedFor.IsUnknown() {
		return
	}
	if !creating && (state.Inputs.IsUnknown() || state.Outputs.IsUnknown() ||
		state.TriggersReset.IsUnknown() || state.TriggersReplacement.IsUnknown() ||
		state.ExpiresAfter.IsUnknown() || state.DetailedOutputs.IsUnknown() ||
		state.DelayedFor.IsUnknown() || state.Accumulated.IsUnknown()) {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(plan.DelayedFor, plan.ExpiresAfter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	var priorAccumulated map[string]string
	var prior stamps
	args := stampArgs{
		PlanInputs:     accumulate.SortedKeys(planInputs),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if !creating {
		priorAccumulated, d = state.priorAccumulated(ctx)
		resp.Diagnostics.Append(d...)
		prior, d = stampsFromMap(ctx, state.DetailedOutputs)
		resp.Diagnostics.Append(d...)
		args.PriorExpires = int64Ptr(state.ExpiresAfter)
		args.PriorDelay = int64Ptr(state.DelayedFor)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	replacing := !creating && !plan.TriggersReplacement.Equal(state.TriggersReplacement)
	reseed := creating || replacing || !plan.TriggersReset.Equal(state.TriggersReset)

	merged := accumulate.NextMapOutputs(reseed, planInputs, priorAccumulated)

	// A reseed discards history: no prior store and no prior stamps, so
	// every key enters with a full delay window.
	if !reseed {
		args.PriorAccumulated = accumulate.SortedKeys(priorAccumulated)
		args.Prior = prior
	}

	visible, detailed, d := planStamps(ctx, accumulate.SortedKeys(merged), args)
	resp.Diagnostics.Append(d...)
	acc, d := mapFromStringMap(ctx, merged)
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(merged, visible))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	if creating || replacing {
		plan.ID = types.StringValue(accumulate.HashMapID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// Read culls expired keys and reveals due ones: the refresh-time half of
// both designs. Read cannot see configuration, so both decisions run on
// stored stamps alone. It writes state only when a key was dropped or
// revealed, or when state lacks accumulated and needs it filled from
// outputs, which keeps the plan after a refresh empty.
func (r *mapResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mapResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values, d := state.priorAccumulated(ctx)
	resp.Diagnostics.Append(d...)
	prior, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys := accumulate.SortedKeys(values)
	now := clockNow()
	survivors, expires := accumulate.FilterExpired(keys, prior.Expires, now)
	visible, pending := accumulate.FilterPending(survivors, prior.Visible, now)
	if !state.Accumulated.IsNull() && len(survivors) == len(keys) && len(pending) == len(prior.Visible) {
		return
	}

	acc, d := mapFromStringMap(ctx, accumulate.Project(values, survivors))
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(values, visible))
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps{Expires: expires, Visible: pending})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Accumulated = acc
	state.Outputs = out
	state.DetailedOutputs = detailed
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update realizes the plan. On the known path every time-dependent decision
// was already made by ModifyPlan (membership and keeper stamps), so Update
// reproduces them through the same pure functions and resolves only the
// fresh entries (unknown in the plan) with the apply clock. The fallback
// path runs only when the plan is unknown, where the plan promised unknown
// and Update is free to decide with the apply clock through applyFallback.
func (r *mapResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	priorAccumulated, d := state.priorAccumulated(ctx)
	resp.Diagnostics.Append(d...)
	prior, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	reseed := !plan.TriggersReset.Equal(state.TriggersReset)
	args := stampArgs{
		PlanInputs:     accumulate.SortedKeys(planInputs),
		PriorExpires:   int64Ptr(state.ExpiresAfter),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PriorDelay:     int64Ptr(state.DelayedFor),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if !reseed {
		args.PriorAccumulated = accumulate.SortedKeys(priorAccumulated)
		args.Prior = prior
	}
	now := clockNow()

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		accumulated, d := stringMapFromMap(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulate.SortedKeys(accumulated), args, now)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextMapOutputs(reseed, planInputs, priorAccumulated)
	survivors, visible, detailed, d := applyFallback(ctx, accumulate.SortedKeys(merged), args, now)
	resp.Diagnostics.Append(d...)
	acc, d := mapFromStringMap(ctx, accumulate.Project(merged, survivors))
	resp.Diagnostics.Append(d...)
	out, d := mapFromStringMap(ctx, accumulate.Project(merged, visible))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op. The framework removes the resource from state and there
// is nothing external to tear down.
func (r *mapResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// ImportState seeds the map from a JSON object. id hashes the seeded
// outputs. inputs is seeded as an empty map rather than null: it is a
// required attribute, and an empty map is a valid non-null value. The next
// plan sets inputs from configuration and Update merges it into the seeded
// outputs. An inputs key in the ID is accepted and ignored.
func (r *mapResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseMapImport(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := mapFromStringMap(ctx, map[string]string{})
	resp.Diagnostics.Append(d...)
	seeded := accumulate.MergeMap(nil, seed.Outputs)
	outputs, d := mapFromStringMap(ctx, seeded)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, accumulate.SortedKeys(seeded), stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &mapResourceModel{
		Inputs:              inputs,
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		DelayedFor:          types.Int64Null(),
		Accumulated:         outputs,
		Outputs:             outputs,
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(accumulate.HashMapID(seeded)),
	})...)
}
```

- [ ] **Step 4: Register the resource**

In `internal/provider/provider.go`, change

```go
	return []func() resource.Resource{
		NewListResource,
		NewSetResource,
	}
```

to

```go
	return []func() resource.Resource{
		NewListResource,
		NewSetResource,
		NewMapResource,
	}
```

- [ ] **Step 5: Run the house tests to verify they pass**

Run: `go test ./internal/provider/ -run 'TestProviderSchema|TestUserFacingStringsUseEmDashes|TestEveryGoFileHasTheSPDXHeader' -v`
Expected: PASS for all three. If `TestUserFacingStringsUseEmDashes` fails, a description contains `--`; replace it with `—`.

- [ ] **Step 6: Write the Read unit test for null accumulated**

Append to `internal/provider/read_test.go`:

```go
// TestMapReadFillsNullAccumulated pins spec section 10 for the map
// resource: state with accumulated null (hand-edited or minimal) refreshes
// with accumulated filled from outputs, values intact.
func TestMapReadFillsNullAccumulated(t *testing.T) {
	ctx := context.Background()
	sch := resourceSchema(t, &mapResource{})
	stringType := tftypes.String
	mapType := tftypes.Map{ElementType: stringType}
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(mapType, map[string]tftypes.Value{"a": tftypes.NewValue(stringType, "1")}),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"expires_after":        tftypes.NewValue(tftypes.Number, nil),
		"delayed_for":          tftypes.NewValue(tftypes.Number, nil),
		"accumulated":          tftypes.NewValue(mapType, nil),
		"outputs":              tftypes.NewValue(mapType, map[string]tftypes.Value{"a": tftypes.NewValue(stringType, "1")}),
		"detailed_outputs": tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
			"a": tftypes.NewValue(detailType, map[string]tftypes.Value{
				"expires_at": tftypes.NewValue(stringType, nil),
				"visible_at": tftypes.NewValue(stringType, nil),
			}),
		}),
		"id": tftypes.NewValue(stringType, "x"),
	})
	req := resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: sch}}
	resp := resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: sch}}
	(&mapResource{}).Read(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %+v", resp.Diagnostics)
	}
	var state mapResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("State.Get: %+v", diags)
	}
	got, _ := stringMapFromMap(ctx, state.Accumulated)
	if want := map[string]string{"a": "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("accumulated after Read = %v, want %v", got, want)
	}
	out, _ := stringMapFromMap(ctx, state.Outputs)
	if !reflect.DeepEqual(out, map[string]string{"a": "1"}) {
		t.Fatalf("outputs after Read = %v, want map[a:1]", out)
	}
}
```

- [ ] **Step 7: Run the unit tests to verify they pass**

Run: `go test ./internal/... `
Expected: PASS (acceptance tests skip without `TF_ACC`).

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/resource_map.go internal/provider/provider.go internal/provider/provider_test.go internal/provider/read_test.go
git commit -m "feat: add accumulator_map resource"
```

---

### Task 6: Acceptance tests, membership and value rules

**Files:**
- Create: `internal/provider/resource_map_test.go`

**Interfaces:**
- Consumes: the registered `accumulator_map` resource (Task 5); shared helpers in `provider_test.go` (`testAccPreCheck`, `testAccProtoV6ProviderFactories`, `expectEmptyAfterRefresh`, `expectDetailed`, `expectPlanDetailed`, `nullDetail`, `pendingDetail`, `stampedDetail`, `setDetail`, `expectAccumulated`, `expectPlanAccumulated`, `expectPlanDetailUnknown`, `expectPlanDetailVisibleUnknown`, `sleepPastExpiry`).
- Produces: test helpers `mapConfig`, `stringMap`, `expectMapOutputs`, `expectMapPlanOutputs`, `expectMapAccumulated`, `expectMapPlanAccumulated`, `expectMapPlanID` used by Tasks 7 to 9.

- [ ] **Step 1: Write the helpers and the membership tests**

Create `internal/provider/resource_map_test.go`:

```go
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
				// Same values re-supplied: the plan is empty.
				Config: mapConfig(`{ a = "1", b = "2" }`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
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
```

- [ ] **Step 2: Run the tests**

Run: `make testacc TESTARGS='-run TestAccMap'`
Expected: PASS for every `TestAccMap*` test. If `TestAccMapOutputsUnknownWhenInputsUnknown` fails on `id`, check that `mapContainsUnknown` walks elements (Task 4).

- [ ] **Step 3: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/resource_map_test.go
git commit -m "test: cover accumulator_map membership and overwrite rules"
```

---

### Task 7: Acceptance tests, expiry

**Files:**
- Edit: `internal/provider/resource_map_test.go` (append)

**Interfaces:**
- Consumes: helpers from Task 6 and the shared stamp checks in `provider_test.go`.
- Produces: nothing new.

- [ ] **Step 1: Write the expiry tests**

Append to `internal/provider/resource_map_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests**

Run: `make testacc TESTARGS='-run TestAccMap'`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/resource_map_test.go
git commit -m "test: cover accumulator_map expiry"
```

---

### Task 8: Acceptance tests, delay and the C1 check

**Files:**
- Edit: `internal/provider/resource_map_test.go` (append; add `"regexp"` to the import block)

**Interfaces:**
- Consumes: helpers from Task 6 and the shared stamp checks in `provider_test.go`.
- Produces: nothing new.

- [ ] **Step 1: Write the delay tests**

Add `"regexp"` to the import block of `internal/provider/resource_map_test.go`, then append:

```go
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
```

- [ ] **Step 2: Run the tests**

Run: `make testacc TESTARGS='-run TestAccMap'`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/resource_map_test.go
git commit -m "test: cover accumulator_map delayed_for"
```

---

### Task 9: Acceptance tests, import

**Files:**
- Edit: `internal/provider/resource_map_test.go` (append; add `"github.com/hashicorp/terraform-plugin-testing/terraform"` to the import block)

**Interfaces:**
- Consumes: `ImportState` from Task 5 and helpers from Task 6.
- Produces: nothing new.

- [ ] **Step 1: Write the import tests**

Add `"github.com/hashicorp/terraform-plugin-testing/terraform"` to the import block, then append:

```go
// mapImportConfig is the configuration the import tests settle against.
const mapImportConfig = `
resource "accumulator_map" "test" {
  inputs = { d = "4" }
}
`

// TestAccMapImport seeds outputs from the JSON ID: id hashes the seeded
// map, inputs is an empty map, and the settling apply merges the
// configured inputs into the seeded outputs.
func TestAccMapImport(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             mapImportConfig,
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
				Config: mapImportConfig,
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
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             mapImportConfig,
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
				Config:            mapImportConfig,
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"a": "1", "d": "4"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccMapImportEmptyOutputs pins Review Focus 5: {"outputs":{}} seeds
// empty maps and the hash of "{}", and the first apply merges the config.
func TestAccMapImportEmptyOutputs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             mapImportConfig,
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
				Config:            mapImportConfig,
				ConfigStateChecks: []statecheck.StateCheck{expectMapOutputs(map[string]string{"d": "4"})},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccMapImportThenDelayedFor pins spec section 9: imported keys seed
// visible, and the first new key in the settling apply is delayed.
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

// TestAccMapImportMalformed pins the map-shaped error messages, including
// the list/set array shape, which the map resource must reject.
func TestAccMapImportMalformed(t *testing.T) {
	for label, tc := range map[string]struct {
		id     string
		expect *regexp.Regexp
	}{
		"bad json":    {`not-json`, regexp.MustCompile(`must be a JSON object`)},
		"unknown key": {`{"outupts":{"a":"1"}}`, regexp.MustCompile(`not recognized`)},
		"array shape": {`{"outputs":["a","b"]}`, regexp.MustCompile(`must be an object of strings`)},
		"number value": {`{"outputs":{"a":1}}`, regexp.MustCompile(`must be an object of strings`)},
	} {
		tc := tc
		t.Run(label, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config:            mapImportConfig,
					ResourceName:      "accumulator_map.test",
					ImportState:       true,
					ImportStateId:     tc.id,
					ImportStateVerify: false,
					ExpectError:       tc.expect,
				}},
			})
		})
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `make testacc TESTARGS='-run TestAccMap'`
Expected: PASS.

- [ ] **Step 3: Run the whole suite once**

Run: `make testacc`
Expected: PASS for every test, list and set included.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/resource_map_test.go
git commit -m "test: cover accumulator_map import"
```

---

### Task 10: Examples, generated docs, README

**Files:**
- Create: `examples/resources/accumulator_map/resource.tf`
- Create: `examples/resources/accumulator_map/import.sh`
- Edit: `README.md:3-6`, `README.md:27-37`, `README.md:60-72`
- Edit: `templates/index.md.tmpl:3-9`
- Generated: `docs/resources/map.md`, `docs/index.md` (via `make docs`)

**Interfaces:**
- Consumes: the registered resource and its schema descriptions (Task 5).
- Produces: nothing new.

- [ ] **Step 1: Write the example**

Create `examples/resources/accumulator_map/resource.tf`:

```hcl
resource "accumulator_map" "last_digest" {
  inputs        = { (var.service) = var.image_digest }
  delayed_for   = 600
  expires_after = 3600
}

output "last_digest" {
  value = accumulator_map.last_digest.outputs
}

output "last_digest_all" {
  value = accumulator_map.last_digest.accumulated
}

output "last_digest_detail" {
  value = accumulator_map.last_digest.detailed_outputs
}
```

Create `examples/resources/accumulator_map/import.sh`:

```sh
# accumulator_map seeds outputs from a JSON object of strings and hashes it
# for id. An inputs key, if present, is accepted and ignored: inputs is
# seeded as an empty map, and the first plan after import supplies it from
# configuration and merges it into the seeded outputs.
terraform import accumulator_map.last_digest '{"outputs":{"api":"sha256:ab","web":"sha256:cd"}}'
```

- [ ] **Step 2: Update the README**

In `README.md`, replace the first paragraph's last sentence

```
no endpoint, no credential, no database, and no object store: the provider is
two resources and a small amount of pure list and set logic.
```

with

```
no endpoint, no credential, no database, and no object store: the provider is
three resources and a small amount of pure list, set, and map logic.
```

In the `## Resources` section, append after the `accumulator_set` bullet:

```
- **`accumulator_map`** remembers every key it has seen with the most recent
  value supplied for it. Re-supplying a key with a different value overwrites
  the stored value in place; the key keeps its identity, so it is never
  delayed or expired by a value change. Keys are forgotten by a
  `triggers_reset` change, a replacement, or (with `expires_after` set)
  expiry.
```

In the `## Expiration` section, change the three occurrences of "Both resources" to "All three resources", and change "A value that leaves `inputs`" to "A value (or map key) that leaves `inputs`".

In the import block, add a third line:

```sh
tofu import accumulator_list.recent_deploys '{"inputs":["a","b"],"outputs":["a","b","c"]}'
tofu import accumulator_set.seen_hosts '{"outputs":["a","b"]}'
tofu import accumulator_map.last_digest '{"outputs":{"api":"sha256:ab","web":"sha256:cd"}}'
```

and append to the paragraph that follows it:

```
For `accumulator_map`, the seed values are JSON objects of strings; `inputs`
is seeded as an empty map and the first apply merges the configured `inputs`
into the seeded `outputs`.
```

In `## Documentation`, change "lists both resources" to "lists all three resources".

- [ ] **Step 3: Update the index template**

In `templates/index.md.tmpl`, replace lines 3 to 9 with:

```
Resources that accumulate a value over successive applies — a list bounded by
a fixed history length, a set, and a map keyed by input key — with an optional
per-value TTL (`expires_after`) for values that stop being supplied, and an
optional warm-up window (`delayed_for`) before a new value appears in
`outputs`. The accumulated history lives in Terraform/OpenTofu state, so there
is no endpoint, no credential, and no external store to stand up: the provider
is three resources and a small amount of pure list, set, and map logic.
```

- [ ] **Step 4: Generate the docs**

Run: `make docs`
Expected: a new `docs/resources/map.md` appears and `docs/index.md` changes. If `tofu` is missing or older than 1.11, install OpenTofu 1.12.x first; CI pins 1.12.4.

- [ ] **Step 5: Check the generated page**

Run: `rg -n "accumulator_map|map\(string\)|Map of String" docs/resources/map.md | head`
Expected: the page title is `accumulator_map (Resource)`, `inputs` is `(Map of String)`, and the import block shows the `import.sh` command. Then run `git status --short` and confirm only the files listed in this task changed.

- [ ] **Step 6: Run everything once more**

Run: `go test ./... && gofmt -l . && go vet ./...`
Expected: PASS, no gofmt output, no vet output.

- [ ] **Step 7: Commit**

```bash
git add examples/resources/accumulator_map/resource.tf examples/resources/accumulator_map/import.sh README.md templates/index.md.tmpl docs/resources/map.md docs/index.md
git commit -m "docs: document accumulator_map"
```

---

## Self-review notes

- **Spec coverage.** Section 3 and 4 (merge, overwrite, key identity): Tasks 1, 5, 6. Expiry rules: Task 7. Delay rules and C1: Task 8. Section 5 schema: Task 5 with the schema house test. Section 6 mechanics: Task 5. Section 7 functions: Tasks 1 to 3. Section 8 helpers: Task 4. Section 9 import: Tasks 3, 5, 9. Section 10 null `accumulated`: Task 5 step 6. Section 11 tests: Tasks 1 to 9. Section 12 docs: Task 10. The provider `MarkdownDescription` in `provider.go` does not count resources, so it needs no change.
- **Type consistency.** `applyFallback` returns `([]string, []string, types.Map, diag.Diagnostics)` as `(accumulated keys, visible keys, detailed, diags)`; Task 5 names them `survivors, visible`. `planStamps` returns `([]string, types.Map, diag.Diagnostics)`; Task 5 names the first `visible`. `stampArgs.PlanInputs` and `PriorAccumulated` are `[]string`, fed with `accumulate.SortedKeys`.
- **Review Focus.** Item 1: `TestAccMapEmptyInputs` (Task 6). Item 2: `TestAccMapKeysWithSeparators` (Task 6). Item 3: `TestMergeMap` "empty string value" (Task 1) and the third step of `TestAccMapOverwriteVisibleKey` (Task 6). Item 4: `TestAccMapValueChangeDoesNotStamp` (Task 7). Item 5: `TestAccMapImportEmptyOutputs` (Task 9).
