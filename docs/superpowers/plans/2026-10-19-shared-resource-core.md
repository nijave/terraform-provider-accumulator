# Shared Resource Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to execute this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two stacked refactors with no behavior change. Part A replaces the three parallel expiry and delay acceptance suites with one table-driven matrix that runs every identity-level row against `accumulator_list`, `accumulator_set`, and `accumulator_map`. Part B moves the phase glue that the three resources copy (the ModifyPlan guards and branch decision, Read's cull and reveal, the Create/Update known and fallback paths, the import tail) into one shared core, so each resource keeps only its schema, its model, and thin method bodies.

**Architecture:** Part A adds `internal/provider/shared_test.go`: a `flavor` adapter per resource (config renderer, address, a `values` renderer that turns a canonical identity list into the right `knownvalue` check, and an import-ID renderer) and a `sharedCase` table that `TestAccShared` runs as `TestAccShared/<row>/<flavor>`. Rows write expected collections in list semantics (order and duplicates kept); the set and map flavors deduplicate, and the map flavor pairs every key with the value `"1"`. Part B adds `internal/provider/core.go`: an embedded `coreModel` with the six shared attributes; a `shape[C, V]` value per resource that holds closures only (decode, encode, keys, project, hash, parse) and never touches a model; and four phase functions, `planPhase`, `applyPhase`, `readPhase`, and `importPhase`, that take the shared `*coreModel` plus the resource's collections and return encoded values for the resource to assign. The pure functions in `internal/accumulate` and the compositions in `stamps.go` (`planStamps`, `applyStamps`, `applyFallback`) are called with the same arguments in the same order as today.

**Tech Stack:** Go 1.26, terraform-plugin-framework v1.19 (embedded structs in `tfsdk` models are supported by value, including unexported ones), terraform-plugin-testing v1.16, OpenTofu >= 1.10 for acceptance tests (`make testacc`), `tools/gen-schema.sh` and `make docs` (OpenTofu >= 1.11 on PATH).

**Spec:**

- Approved design: `/tmp/claude-1000/-home-nick-Documents-workspace-go-src-github-com-nijave-terraform-provider-accumulator/14f67810-b62e-4f79-8adf-f0c5cb16a36a/scratchpad/architecture-review.md`, section 2 (phase core sketch), section 7 (shared matrix), and recommendations 4 and 5.
- Binding behavior: `docs/superpowers/specs/2026-09-23-accumulator-expiration-design.md`, `docs/superpowers/specs/2026-10-15-accumulator-delayed-for-design.md`, `docs/superpowers/specs/2026-10-18-accumulator-map-design.md`. Neither part changes a planned or applied value, a state shape, a diagnostic text, or a schema description.

**Base:** `refactor/accumulate-cleanups` at `640d696` (it has `accumulate.Union`, the shared `parseSeed`/`hashJSON` bodies behind the existing exported names, and the map import already uses `seed.Outputs` directly). Create Part A on `test/shared-matrix` from that commit, and Part B on `refactor/shared-core` from the tip of Part A. If `refactor/accumulate-cleanups` has merged into `feat/accumulator-map` by then, branch from the merge instead; nothing below depends on the commit id.

## Global Constraints

- Every Go file starts with `// SPDX-License-Identifier: MPL-2.0` (house test `TestEveryGoFileHasTheSPDXHeader`).
- No `--` inside any string literal in non-test files of `internal/provider`; write a real em dash `—` (house test `TestUserFacingStringsUseEmDashes`). Part B moves no user-facing strings, and every `MarkdownDescription` stays byte-identical.
- Part A edits only `_test.go` files. Part B edits no existing test and no acceptance test; it adds `golden_test.go`, `core_test.go`, and `testdata/phases.golden`.
- State compatibility: no `tfsdk` tag, attribute name, or attribute type changes. `TestProviderSchema`, `TestStateGetToleratesMissingDelayAttributes`, the three `Read*FillsNullAccumulated` tests, and `TestModelsRoundTripThroughEmbeddedCore` prove it.
- Determinism: `TestPhasesGolden` (Task B1) must pass unchanged after every Part B task. Never rerun it with `-update` after Task B1.
- ModifyPlan reads no clock. Only `Read`, `Create`, and `Update` call `clockNow()`, now inside `readPhase` and `applyPhase`.
- Collections written to state are never null: `[]` and `{}`, never null.
- Run unit tests with `go test ./internal/...`. Run acceptance tests with `make testacc` (needs `tofu` on PATH); filter with `TESTARGS='-run <regexp>'`. Run `gofmt -l .` (expect no output) and `go vet ./...` before each commit.
- Commit messages follow the repository's Conventional Commits style (`test:`, `refactor:`), imperative, no trailing period. Stage explicit paths only; never `git add -A` or `git add .`.

## Review Focus

1. **Rows are written in list semantics.** A row such as `f.expectOutputs("a", "b", "a")` is the list's exact value; the set flavor checks `SetExact(a, b)` and the map flavor `MapExact({a: "1", b: "1"})`. Read every row's expected values as "what the list holds"; the deduplication is in `flavor.values` (Task A4) and nowhere else.
2. **Every merged row keeps every check of every source test.** Where two sources checked different things at the same step, the row checks both (the union), and Task A5 Step 6 runs it green on all three resources. The inventory table lists, per test, the row it became and every literal that changed. The literal changes are confined to inputs that carry no collection-specific meaning.
3. **`TestAccMapReSupplyRescues` stays in the map file** although a matrix row has the same name: it re-supplies a *stamped, not yet expired* key with a *new value*, which is map overwrite behavior. The row `ReSupplyRescues` re-supplies after the window passed with the same value.
4. **No reordering in Part B.** The first draft of this item claimed each resource could decode its planned `inputs` before `planPhase` ran the C1 check. Review of Task B4 showed that a known collection holding a null element does emit a decode diagnostic, which would have turned an unknown-at-plan result into an error. `planPhase` therefore decodes `inputs` through the `decodeInputs` closure in `planCall`, after the unknown guards and after C1, exactly where the old bodies decoded it; `TestModifyPlanNullInputElementWithUnknownBranchInput` pins the case and `TestPhasesGolden` pins the C1 diagnostic text for all three resources.
5. **`shape` has two type parameters, not one.** The review's sketch is `shape[C]`. `encode` must return the framework type of `accumulated`/`outputs` (`types.List`, `types.Set`, or `types.Map`), and Go cannot derive that type from `C` (the list and the set share `C = []string`). The alternatives were setter closures over the model, which the review rules out, or an encode call pair repeated at every phase exit in every resource. `V` is fixed per resource in one `var` declaration, so callers never write the type arguments except in the `planCall`/`applyCall` literals.
6. **The golden masks clock values but not the wiring the review flagged.** A swapped `PriorExpires` (plan instead of state) changes a planned `expires_at` from `<unknown>` to a known `"2999-01-01T00:00:00Z"`, which the golden catches. Task B1 Step 5 proves it with a deliberate mutation, and Task A4 Step 8 proves the same for the acceptance matrix.

---

# Part A: shared acceptance-test matrix

## Acceptance test inventory

Counted with `grep -c '^func TestAcc' internal/provider/*_test.go` at the base commit: **122** top-level acceptance tests (list 42, set 38, map 37, import 5).

After Part A: **54** top-level acceptance tests (list 22, set 10, map 13, import 8, shared 1). `TestAccShared` runs 28 rows × 3 flavors = 84 subtests. `TestAccImportMalformed` runs 10 subtests (3 list, 3 set, 4 map). The suite took 1m58s before and 2m20s after on the reference machine; the growth is the 16 row × flavor pairs that had no test before (8 for the map, 8 for the list).

Rows are numbered E1 to E16 (expiry, Task A4) and D1 to D12 (delay, Task A5). "Kept" means the test stays in its file unchanged except for the `setDetail` rename. "Union" lists the checks a row adds over that source; each added check comes from another source of the same row. A literal change is named only where the source's inputs differ from the row's.

### accumulator_list (42 → 22)

| Existing test | Becomes | Notes |
|---|---|---|
| TestAccListCreate | kept | |
| TestAccListTrimsOnCreate | kept | length |
| TestAccListAppendAndTrim | kept | length, order |
| TestAccListEmptyInputsRetainsHistory | kept | |
| TestAccListLengthShrinkAndGrow | kept | length |
| TestAccListLengthZero | kept | length |
| TestAccListUnchangedInputs | kept | |
| TestAccListOutputsKnownAtPlanTimeOnCreate | kept | |
| TestAccListOutputsKnownAtPlanTimeOnAppend | kept | |
| TestAccListOutputsKnownAtPlanTimeOnLengthChange | kept | length |
| TestAccListOutputsKnownAtPlanTimeOnReset | kept | |
| TestAccListOutputsKnownAtPlanTimeOnReplacement | kept | |
| TestAccListOutputsUnknownWhenInputsUnknown | kept | |
| TestAccListFeedsAnotherListAtPlanTime | kept | |
| TestAccListReset | kept | |
| TestAccListReplacement | kept | |
| TestAccListCreateWithExpiresAfter | E1 CreateWithExpiresAfter/list | Literal: `["a"]`, length 5 → `["a", "b"]`, length 10. Same four checks. |
| TestAccListLeaverStampedOnInputsChange | kept | Two leavers and an appended `c`, order preserved: list semantics. The list also runs E2. |
| TestAccListExpiryRealizedOnRefresh | E3 ExpiryRealizedOnRefresh/list | Literal: `["a"]` → `["b"]` becomes `["a", "b"]` → `["a"]`, so the stamped leaver is `b` and the survivor history is `a, a`. Every check kept (step 2 outputs and detailed with one stamped and one null entry; step 3 empty pre-apply and post-refresh plans, outputs, detailed). Union: step 3 `accumulated` (map source). |
| TestAccListDuplicatesExpireTogether | kept | duplicates |
| TestAccListInInputsNeverExpires | E4 InInputsNeverExpires/list | Identical. |
| TestAccListExpiresAfterSetOnExistingResource | E5 ExpiresAfterSetOnExistingResource/list | Identical. |
| TestAccListExpiresAfterRemoved | E6 ExpiresAfterRemoved/list | Identical. |
| TestAccListExpiresAfterZeroCullsNextRefresh | E7 ExpiresAfterZeroCullsNextRefresh/list | Identical. |
| TestAccListExpiresAfterDecreasePullsEarlier | E8 ExpiresAfterDecreasePullsEarlier/list | Identical. |
| TestAccListIncreaseDoesNotResurrect | E9 IncreaseDoesNotResurrect/list | Identical. |
| TestAccListExpiresAfterIncreaseKeepsLive | E10 ExpiresAfterIncreaseKeepsLive/list | Identical. |
| TestAccListReSupplyRescues | E11 ReSupplyRescues/list | Identical. |
| TestAccListResetClearsStamps | E12 ResetClearsStamps/list | Union: step 2 detailed (map source). |
| TestAccListReplacementClearsStamps | E13 ReplacementClearsStamps/list | Identical. |
| TestAccListUnknownExpiresAfterAtPlan | E14 UnknownExpiresAfterAtPlan/list | Literal: length 1 → 10. Same checks. |
| TestAccListImportThenExpiresAfter | E15 ImportThenExpiresAfter/list | Literal: length 3 → 10; the seed `{"inputs":["a"],"outputs":["a","b"]}` is unchanged. Same checks. |
| TestAccListExpiresAfterUpperBound | E16 ExpiresAfterUpperBound/list | Literal: length 1 → 10. |
| TestAccListCreateWithDelayedFor | kept | Length trims `accumulated`, not `outputs`. The list also runs D1. |
| TestAccListOutputsKnownAtPlanTimeWithDelayedFor | kept | Has a length-only step. The list also runs D2. |
| TestAccListRevealRealizedOnRefresh | kept | Duplicates reveal together. The list also runs D3. |
| TestAccListRevealKeepsHistoryOrder | kept | order |
| TestAccListDelayedForRemoved | D4 DelayedForRemoved/list | Union: step 2 detailed (map source). |
| TestAccListDelayedForZeroRevealsNextRefresh | D7 DelayedForZeroRevealsNextRefresh/list | Union: step 2 post-refresh empty plan (map) and detailed (set). |
| TestAccListDelayedForMustBeShorterThanExpiresAfter | D8 DelayedForMustBeShorterThanExpiresAfter/list | Union: the greater and shorter cases (set and map sources). |
| TestAccListPendingLeaverStamped | D9 PendingLeaverStamped/list | Union: step 2 outputs (set source). |
| TestAccListUnknownDelayedForAtPlan | D11 UnknownDelayedForAtPlan/list | Union: detailed (set source). |

### accumulator_set (38 → 10)

| Existing test | Becomes | Notes |
|---|---|---|
| TestAccSetOutputsKnownAtPlanTimeOnCreate | kept | |
| TestAccSetOutputsKnownAtPlanTimeOnUnion | kept | |
| TestAccSetOutputsKnownAtPlanTimeOnReset | kept | |
| TestAccSetOutputsUnknownWhenInputsUnknown | kept | |
| TestAccSetFeedsFromListAtPlanTime | kept | |
| TestAccSetUnion | kept | dedup |
| TestAccSetDeduplicatesWithinOneApply | kept | dedup |
| TestAccSetReset | kept | |
| TestAccSetReplacement | kept | |
| TestAccSetCreateIdentifiesFromInputs | kept | |
| TestAccSetCreateWithExpiresAfter | E1 CreateWithExpiresAfter/set | Identical. |
| TestAccSetLeaverStampedOnInputsChange | E2 LeaverStampedOnInputsChange/set | Identical (the row's `a, b, a` deduplicates to the source's `{a, b}`). |
| TestAccSetExpiryRealizedOnRefresh | E3 ExpiryRealizedOnRefresh/set | Union: step 3 accumulated (map source). |
| TestAccSetInInputsNeverExpires | E4 InInputsNeverExpires/set | Identical. |
| TestAccSetExpiresAfterSetOnExistingResource | E5 ExpiresAfterSetOnExistingResource/set | Union: step 2 planned outputs (list source). |
| TestAccSetExpiresAfterRemoved | E6 ExpiresAfterRemoved/set | Identical. |
| TestAccSetExpiresAfterZeroCullsNextRefresh | E7 ExpiresAfterZeroCullsNextRefresh/set | Identical. |
| TestAccSetExpiresAfterDecreasePullsEarlier | E8 ExpiresAfterDecreasePullsEarlier/set | Identical. |
| TestAccSetIncreaseDoesNotResurrect | E9 IncreaseDoesNotResurrect/set | Identical. |
| TestAccSetReSupplyRescues | E11 ReSupplyRescues/set | Union: step 3 planned outputs (list source). |
| TestAccSetResetClearsStamps | E12 ResetClearsStamps/set | Union: step 2 detailed (map source). |
| TestAccSetReplacementClearsStamps | E13 ReplacementClearsStamps/set | Identical. |
| TestAccSetUnknownExpiresAfterAtPlan | E14 UnknownExpiresAfterAtPlan/set | Identical. |
| TestAccSetImportThenExpiresAfter | E15 ImportThenExpiresAfter/set | Union: step 2 planned outputs (list source). |
| TestAccSetExpiresAfterIncreaseKeepsLive | E10 ExpiresAfterIncreaseKeepsLive/set | Identical. |
| TestAccSetExpiresAfterUpperBound | E16 ExpiresAfterUpperBound/set | Identical. |
| TestAccSetCreateWithDelayedFor | D1 CreateWithDelayedFor/set | Identical. |
| TestAccSetOutputsKnownAtPlanTimeWithDelayedFor | D2 OutputsKnownAtPlanTimeWithDelayedFor/set | Union: step 2 planned `visible_at` unknown for `b`, and detailed (from TestAccMapEnteringKeyHidden). |
| TestAccSetRevealRealizedOnRefresh | D3 RevealRealizedOnRefresh/set | Identical. |
| TestAccSetDelayedForRemoved | D4 DelayedForRemoved/set | Union: step 2 detailed (map source). |
| TestAccSetDelayedForDecreaseRevealsSooner | D5 DelayedForDecreaseRevealsSooner/set | Identical. |
| TestAccSetDelayedForIncreaseKeepsPending | D6 DelayedForIncreaseKeepsPending/set | Identical. |
| TestAccSetDelayedForZeroRevealsNextRefresh | D7 DelayedForZeroRevealsNextRefresh/set | Union: step 2 post-refresh empty plan (map source). |
| TestAccSetDelayedForMustBeShorterThanExpiresAfter | D8 DelayedForMustBeShorterThanExpiresAfter/set | The equal, greater, and shorter subtests become three steps of one case; each rejected step creates nothing, so the shorter step is still a create. |
| TestAccSetPendingLeaverStamped | D9 PendingLeaverStamped/set | Identical. |
| TestAccSetResetDelaysSeed | D10 ResetDelaysSeed/set | Literal: steps 1 and 2 (`delayed_for = 1`, sleep, `outputs = [a]`) are D3's steps and checks, so D10 instead reaches a visible `a` the map source's way, with `triggers_reset = "v1"` and no delay; the reset is `"v1"` → `"v2"` instead of null → `"v2"`. Step 3's checks are kept verbatim. |
| TestAccSetUnknownDelayedForAtPlan | D11 UnknownDelayedForAtPlan/set | Identical. |
| TestAccSetImportThenDelayedFor | D12 ImportThenDelayedFor/set | Identical. |

### accumulator_map (37 → 13, plus 3 moved and 1 folded)

| Existing test | Becomes | Notes |
|---|---|---|
| TestAccMapOutputsKnownAtPlanTimeOnCreate | kept | |
| TestAccMapEmptyInputs | kept | |
| TestAccMapMerge | kept | overwrite |
| TestAccMapOverwriteVisibleKey | kept | overwrite |
| TestAccMapKeysWithSeparators | kept | |
| TestAccMapReset | kept | |
| TestAccMapReplacement | kept | |
| TestAccMapOutputsUnknownWhenInputsUnknown | kept | |
| TestAccMapFeedsFromSetAtPlanTime | kept | |
| TestAccMapLeaverStampedOnInputsChange | E2 LeaverStampedOnInputsChange/map | Union: planned detailed partial for `a` (set source). |
| TestAccMapValueChangeDoesNotStamp | kept | value change |
| TestAccMapExpiryRealizedOnRefresh | E3 ExpiryRealizedOnRefresh/map | Identical. |
| TestAccMapReSupplyRescues | kept | Re-supply of a stamped key with a new value (Review Focus 3). |
| TestAccMapResetClearsStamps | E12 ResetClearsStamps/map | Union: step 2 outputs (set source). |
| TestAccMapExpiresAfterSetOnExistingResource | E5 ExpiresAfterSetOnExistingResource/map | Union: step 2 planned outputs (list source). |
| TestAccMapExpiresAfterRemoved | E6 ExpiresAfterRemoved/map | Identical. |
| TestAccMapExpiresAfterDecreasePullsEarlier | E8 ExpiresAfterDecreasePullsEarlier/map | Identical. |
| TestAccMapExpiresAfterIncreaseKeepsLive | E10 ExpiresAfterIncreaseKeepsLive/map | Identical. |
| TestAccMapUnknownExpiresAfterAtPlan | E14 UnknownExpiresAfterAtPlan/map | Identical. |
| TestAccMapCreateWithDelayedFor | D1 CreateWithDelayedFor/map | Identical. |
| TestAccMapOverwritePendingKey | kept | overwrite |
| TestAccMapEnteringKeyHidden | D2 OutputsKnownAtPlanTimeWithDelayedFor/map, steps 1 and 2 | Identical for those steps; D2 adds the reset and replacement steps (set source). |
| TestAccMapRevealRealizedOnRefresh | D3 RevealRealizedOnRefresh/map | Identical. |
| TestAccMapDelayedForRemoved | D4 DelayedForRemoved/map | Literal: `{ a = "1" }` → `{ a = "1", b = "1" }`. Same checks, over both keys. |
| TestAccMapDelayedForZeroRevealsNextRefresh | D7 DelayedForZeroRevealsNextRefresh/map | Step 2 drops the source's `sleepPastExpiry()`: a stamp of "now" reveals on the next refresh without waiting, which is the stronger claim (the set source never slept). Union: step 1 planned outputs and detailed, step 2 detailed (set source). |
| TestAccMapDelayedForMustBeShorterThanExpiresAfter | D8 DelayedForMustBeShorterThanExpiresAfter/map | As for the set. |
| TestAccMapPendingLeaverStamped | D9 PendingLeaverStamped/map | Identical. |
| TestAccMapResetDelaysSeed | D10 ResetDelaysSeed/map | Identical. |
| TestAccMapUnknownDelayedForAtPlan | D11 UnknownDelayedForAtPlan/map | Identical. |
| TestAccMapImport | moved to `import_test.go` | Unchanged; reads `importConfig("accumulator_map")`. |
| TestAccMapImportIgnoresInputsKey | moved to `import_test.go` | Unchanged. |
| TestAccMapImportEmptyOutputs | moved to `import_test.go` | Unchanged. |
| TestAccMapImportThenDelayedFor | D12 ImportThenDelayedFor/map | Literal: seed `{"outputs":{"a":"1"}}` → `{"outputs":{"a":"1","b":"1"}}`, configured `d = "4"` → `d = "1"`. Same checks. |
| TestAccMapImportMalformed | folded into TestAccImportMalformed | Its four cases become `TestAccImportMalformed/accumulator_map/<case>`, unchanged. |
| TestAccMapDelayedForDecreaseRevealsSooner | D5 DelayedForDecreaseRevealsSooner/map | Identical. |
| TestAccMapDelayedForIncreaseKeepsPending | D6 DelayedForIncreaseKeepsPending/map | Identical. |
| TestAccMapEmptyStringKey | kept | |

### import_test.go (5 → 8)

| Existing test | Becomes | Notes |
|---|---|---|
| TestAccListImport | kept | |
| TestAccSetImport | kept | |
| TestAccSetImportIgnoresInputsKey | kept | |
| TestAccSetImportHashesDeduplicatedOutputs | kept | |
| TestAccImportMalformed | kept, extended | Gains the map's four cases. |

Rows with no prior test for a flavor (new coverage): map E1, E4, E7, E9, E11, E13, E15, E16; list E2, D1, D2, D3, D5, D6, D10, D12.

---

### Task A1: Rename setDetail to oneDetail

**Files:**
- Modify: `internal/provider/provider_test.go`, `internal/provider/resource_list_test.go`, `internal/provider/resource_set_test.go`, `internal/provider/resource_map_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func oneDetail(value string, check knownvalue.Check) map[string]knownvalue.Check` (renamed from `setDetail`; same body).

- [ ] **Step 1: Rename the helper and every call site**

```bash
sed -i 's/\bsetDetail(/oneDetail(/g' internal/provider/*_test.go
sed -i 's|^// setDetail builds a one-entry map|// oneDetail builds a one-entry map|' internal/provider/provider_test.go
```

The helper in `internal/provider/provider_test.go` now reads:

```go
// oneDetail builds a one-entry map for expectDetailed/expectPlanDetailed.
func oneDetail(value string, check knownvalue.Check) map[string]knownvalue.Check {
	return map[string]knownvalue.Check{value: check}
}
```

- [ ] **Step 2: Verify nothing still uses the old name**

Run: `grep -rn 'setDetail' internal/ ; go vet ./internal/provider/ && go test ./internal/...`
Expected: no `grep` output; `ok` for both packages.

- [ ] **Step 3: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/provider_test.go internal/provider/resource_list_test.go internal/provider/resource_set_test.go internal/provider/resource_map_test.go
git commit -m "test: rename setDetail to oneDetail"
```

---

### Task A2: List Read fills a null accumulated

**Files:**
- Modify: `internal/provider/read_test.go`

**Interfaces:**
- Consumes: `resourceSchema` (`model_test.go`), `listResource.Read`, `stringsFromList`.
- Produces: `TestListReadFillsNullAccumulated`; `TestReadFillsNullAccumulated` renamed to `TestSetReadFillsNullAccumulated`.

- [ ] **Step 1: Rename the set test and name the spec in both comments**

In `internal/provider/read_test.go`, replace

```go
// TestReadFillsNullAccumulated pins spec section 9 for Read: state written
// before this feature arrives with accumulated null (the framework fills a
// missing attribute with null on decode). Read must write accumulated as a
// copy of outputs, so the next plan is empty, and leave everything else
// alone.
func TestReadFillsNullAccumulated(t *testing.T) {
```

with

```go
// TestSetReadFillsNullAccumulated pins
// 2026-10-15-accumulator-delayed-for-design.md section 9 for Read: state
// written before that feature arrives with accumulated null (the framework
// fills a missing attribute with null on decode). Read must write
// accumulated as a copy of outputs, so the next plan is empty, and leave
// everything else alone.
func TestSetReadFillsNullAccumulated(t *testing.T) {
```

and replace

```go
// TestMapReadFillsNullAccumulated pins spec section 10 for the map
// resource:
```

with

```go
// TestMapReadFillsNullAccumulated pins 2026-10-18-accumulator-map-design.md
// section 10 for the map resource:
```

- [ ] **Step 2: Add the list test at the end of the file**

The history has a duplicate and is out of order, so a Read that rebuilt `accumulated` through a set or a sort would fail it.

```go
// TestListReadFillsNullAccumulated pins
// 2026-10-15-accumulator-delayed-for-design.md section 9 for the list
// resource: pre-feature state with accumulated null refreshes with
// accumulated filled from outputs, order and duplicates intact.
func TestListReadFillsNullAccumulated(t *testing.T) {
	ctx := context.Background()
	sch := resourceSchema(t, &listResource{})
	stringType := tftypes.String
	listType := tftypes.List{ElementType: stringType}
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	nullDetail := tftypes.NewValue(detailType, map[string]tftypes.Value{
		"expires_at": tftypes.NewValue(stringType, nil),
		"visible_at": tftypes.NewValue(stringType, nil),
	})
	history := []tftypes.Value{
		tftypes.NewValue(stringType, "b"),
		tftypes.NewValue(stringType, "a"),
		tftypes.NewValue(stringType, "b"),
	}
	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(stringType, "b")}),
		"length":               tftypes.NewValue(tftypes.Number, 5),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"expires_after":        tftypes.NewValue(tftypes.Number, nil),
		"delayed_for":          tftypes.NewValue(tftypes.Number, nil),
		"accumulated":          tftypes.NewValue(listType, nil),
		"outputs":              tftypes.NewValue(listType, history),
		"detailed_outputs": tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
			"a": nullDetail,
			"b": nullDetail,
		}),
		"id": tftypes.NewValue(stringType, "x"),
	})
	req := resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: sch}}
	resp := resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: sch}}
	(&listResource{}).Read(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %+v", resp.Diagnostics)
	}
	var state listResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("State.Get: %+v", diags)
	}
	want := []string{"b", "a", "b"}
	got, _ := stringsFromList(ctx, state.Accumulated)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accumulated after Read = %v, want %v", got, want)
	}
	out, _ := stringsFromList(ctx, state.Outputs)
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("outputs after Read = %v, want %v", out, want)
	}
	if state.Length.ValueInt64() != 5 {
		t.Fatalf("length after Read = %v, want 5", state.Length)
	}
}
```

- [ ] **Step 3: Run the Read tests**

Run: `go test ./internal/provider/ -run 'ReadFillsNullAccumulated' -v`
Expected:

```
--- PASS: TestSetReadFillsNullAccumulated
--- PASS: TestMapReadFillsNullAccumulated
--- PASS: TestListReadFillsNullAccumulated
ok
```

The list test pins existing behavior, so it passes at once. To see it fail, drop the null check from `listResource.Read`'s early return and run it again:

```bash
sed -i 's/if !state.Accumulated.IsNull() \&\& len(survivors)/if true \&\& len(survivors)/' internal/provider/resource_list.go
go test ./internal/provider/ -run TestListReadFillsNullAccumulated 2>&1 | head -2
git checkout -- internal/provider/resource_list.go
```

Expected before the checkout: `--- FAIL: TestListReadFillsNullAccumulated` and `accumulated after Read = [], want [b a b]`.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/read_test.go
git commit -m "test: cover list Read filling a null accumulated"
```

---

### Task A3: Move the map import tests beside list and set

**Files:**
- Modify: `internal/provider/import_test.go`, `internal/provider/resource_map_test.go`, `internal/provider/resource_set_test.go`

**Interfaces:**
- Consumes: `expectMapOutputs`, `expectMapAccumulated`, `expectMapPlanOutputs` (stay in `resource_map_test.go`).
- Produces: `importConfig("accumulator_map")`; `TestAccMapImport`, `TestAccMapImportIgnoresInputsKey`, `TestAccMapImportEmptyOutputs` in `import_test.go`; `importSeedsNullDetails` moved to `import_test.go` (Task A4's matrix uses it); `TestAccImportMalformed` covering all three resources.

- [ ] **Step 1: Teach importConfig the map**

In `internal/provider/import_test.go`, replace the whole `importConfig` function with:

```go
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
```

The map branch is the old `mapImportConfig` constant, so every moved test runs against the same configuration as before.

- [ ] **Step 2: Add the moved map tests and the import helper after TestAccSetImportHashesDeduplicatedOutputs**

Insert these four declarations, in this order, between `TestAccSetImportHashesDeduplicatedOutputs` and `TestAccImportMalformed`. The test bodies are the old ones from `resource_map_test.go` with `mapImportConfig` replaced by `config := importConfig("accumulator_map")`; `importSeedsNullDetails` is moved verbatim from `resource_set_test.go` with its spec reference named.

```go
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
```

- [ ] **Step 3: Fold the map malformed cases into TestAccImportMalformed**

Replace the whole `TestAccImportMalformed` function with:

```go
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
```

- [ ] **Step 4: Set the import block of import_test.go**

```go
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
```

- [ ] **Step 5: Delete the moved declarations from their old files**

From `internal/provider/resource_map_test.go` delete, with their doc comments: `mapImportConfig`, `TestAccMapImport`, `TestAccMapImportIgnoresInputsKey`, `TestAccMapImportEmptyOutputs`, `TestAccMapImportMalformed`. From `internal/provider/resource_set_test.go` delete `importSeedsNullDetails` with its doc comment. Then set the import block of both files (it drops `terraform`, now used only in `import_test.go`):

```go
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
```

- [ ] **Step 6: Build and count**

Run: `go vet ./internal/provider/ && go test ./internal/... && grep -c '^func TestAcc' internal/provider/*_test.go`
Expected: `ok` for both packages; counts `import_test.go:8`, `resource_list_test.go:42`, `resource_map_test.go:33`, `resource_set_test.go:38` (121 in total: the map's malformed test is now subtests of the shared one).

- [ ] **Step 7: Run the import acceptance tests**

Run: `make testacc TESTARGS='-run "TestAcc(List|Set|Map)Import|TestAccImportMalformed"'`
Expected: `--- PASS` for every import test, including `TestAccImportMalformed/accumulator_map/array_shape`, `.../bad_json`, `.../number_value`, `.../unknown_key`, and the three list and three set cases; final line `ok  	github.com/nijave/terraform-provider-accumulator/internal/provider`.

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/import_test.go internal/provider/resource_map_test.go internal/provider/resource_set_test.go
git commit -m "test: move map import tests beside list and set"
```

---

### Task A4: Run the expiry tests against all three resources

**Files:**
- Create: `internal/provider/shared_test.go`
- Modify: `internal/provider/resource_list_test.go`, `internal/provider/resource_set_test.go`, `internal/provider/resource_map_test.go`

**Interfaces:**
- Consumes: `listConfig`, `setConfig`, `mapConfig`, `stringList`, `stringMap`, `expectAccumulated`, `expectPlanAccumulated`, `expectDetailed`, `expectPlanDetailed`, `expectPlanDetailedPartial`, `expectPlanDetailUnknown`, `nullDetail`, `stampedDetail`, `oneDetail`, `sleepPastExpiry`, `expectEmptyAfterRefresh`, `importSeedsNullDetails`, `testAccPreCheck`, `testAccProtoV6ProviderFactories`.
- Produces:
  - `type flavor struct { name, addr string; config func(ids []string, extra string) string; values func(ids []string) knownvalue.Check; importID func(inputs, outputs []string) string }`
  - `func (f flavor) expectOutputs(ids ...string) statecheck.StateCheck`
  - `func (f flavor) expectAccumulated(ids ...string) statecheck.StateCheck`
  - `func (f flavor) expectPlanOutputs(ids ...string) plancheck.PlanCheck`
  - `func (f flavor) expectPlanAccumulated(ids ...string) plancheck.PlanCheck`
  - `var listFlavor, setFlavor, mapFlavor flavor`; `var flavors []flavor`
  - `type sharedCase struct { name string; steps func(f flavor) []resource.TestStep }`
  - `var expiryCases []sharedCase`; `var sharedGroups [][]sharedCase`
  - `func ids(values ...string) []string`; `const unknownSource string`; `const mapValue = "1"`; `const sharedListLength = 10`
  - `func TestAccShared(t *testing.T)`

- [ ] **Step 1: Write the matrix with the expiry rows**

Create `internal/provider/shared_test.go`:

```go
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
var sharedGroups = [][]sharedCase{expiryCases}

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
```

- [ ] **Step 2: Run the new rows before deleting anything**

Run: `make testacc TESTARGS='-run TestAccShared'`
Expected: 48 leaf subtests pass (16 rows × 3 flavors), for example `--- PASS: TestAccShared/ExpiresAfterDecreasePullsEarlier/map`; final line `ok`. If a row fails for one flavor only, the flavor adapter is wrong, not the row: compare the failing check against the source test named in the inventory.

- [ ] **Step 3: Delete the migrated expiry tests from resource_list_test.go**

Delete these functions, each with its doc comment: `TestAccListCreateWithExpiresAfter`, `TestAccListExpiryRealizedOnRefresh`, `TestAccListInInputsNeverExpires`, `TestAccListExpiresAfterSetOnExistingResource`, `TestAccListExpiresAfterRemoved`, `TestAccListExpiresAfterZeroCullsNextRefresh`, `TestAccListExpiresAfterDecreasePullsEarlier`, `TestAccListIncreaseDoesNotResurrect`, `TestAccListExpiresAfterIncreaseKeepsLive`, `TestAccListReSupplyRescues`, `TestAccListResetClearsStamps`, `TestAccListReplacementClearsStamps`, `TestAccListUnknownExpiresAfterAtPlan`, `TestAccListImportThenExpiresAfter`, `TestAccListExpiresAfterUpperBound` (15).

- [ ] **Step 4: Delete the migrated expiry tests from resource_set_test.go**

Delete, each with its doc comment: `TestAccSetCreateWithExpiresAfter`, `TestAccSetLeaverStampedOnInputsChange`, `TestAccSetExpiryRealizedOnRefresh`, `TestAccSetInInputsNeverExpires`, `TestAccSetExpiresAfterSetOnExistingResource`, `TestAccSetExpiresAfterRemoved`, `TestAccSetExpiresAfterZeroCullsNextRefresh`, `TestAccSetExpiresAfterDecreasePullsEarlier`, `TestAccSetIncreaseDoesNotResurrect`, `TestAccSetReSupplyRescues`, `TestAccSetResetClearsStamps`, `TestAccSetReplacementClearsStamps`, `TestAccSetUnknownExpiresAfterAtPlan`, `TestAccSetImportThenExpiresAfter`, `TestAccSetExpiresAfterIncreaseKeepsLive`, `TestAccSetExpiresAfterUpperBound` (16).

- [ ] **Step 5: Delete the migrated expiry tests from resource_map_test.go**

Delete, each with its doc comment: `TestAccMapLeaverStampedOnInputsChange`, `TestAccMapExpiryRealizedOnRefresh`, `TestAccMapResetClearsStamps`, `TestAccMapExpiresAfterSetOnExistingResource`, `TestAccMapExpiresAfterRemoved`, `TestAccMapExpiresAfterDecreasePullsEarlier`, `TestAccMapExpiresAfterIncreaseKeepsLive`, `TestAccMapUnknownExpiresAfterAtPlan` (8).

The import blocks of the three files do not change in this task: each still uses `regexp` (its C1 test moves in Task A5).

- [ ] **Step 6: Build and count**

Run: `gofmt -l . ; go vet ./internal/provider/ && go test ./internal/... && grep -c '^func TestAcc' internal/provider/*_test.go`
Expected: no `gofmt` output; `ok` for both packages; counts `import_test.go:8`, `resource_list_test.go:27`, `resource_map_test.go:25`, `resource_set_test.go:22`, `shared_test.go:1` (83 in total).

- [ ] **Step 7: Run the full acceptance suite**

Run: `make testacc 2>&1 | tee /tmp/accumulator-a4.log | grep -E '^(--- FAIL|ok|FAIL)'`
Expected: no `--- FAIL` line; `ok  	github.com/nijave/terraform-provider-accumulator/internal/provider`.

- [ ] **Step 8: Prove the matrix closes the map wiring gap**

The review's section 7 gap: no map test changed `expires_after` between steps, so passing the planned value as the prior one went unnoticed. Temporarily make that mistake in `mapResource.ModifyPlan`:

```bash
sed -i '0,/args.PriorExpires = int64Ptr(state.ExpiresAfter)/s//args.PriorExpires = int64Ptr(plan.ExpiresAfter)/' internal/provider/resource_map.go
make testacc TESTARGS='-run "TestAccShared/(ExpiresAfterDecreasePullsEarlier|ExpiresAfterIncreaseKeepsLive)/map"' 2>&1 | grep -E '^\s*--- (PASS|FAIL)'
git checkout -- internal/provider/resource_map.go
```

Expected before the checkout:

```
--- FAIL: TestAccShared
    --- FAIL: TestAccShared/ExpiresAfterDecreasePullsEarlier
        --- FAIL: TestAccShared/ExpiresAfterDecreasePullsEarlier/map
    --- FAIL: TestAccShared/ExpiresAfterIncreaseKeepsLive
        --- FAIL: TestAccShared/ExpiresAfterIncreaseKeepsLive/map
```

Then confirm `git status --short internal/provider/resource_map.go` prints nothing.

- [ ] **Step 9: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/shared_test.go internal/provider/resource_list_test.go internal/provider/resource_set_test.go internal/provider/resource_map_test.go
git commit -m "test: run expiry tests against all three resources"
```

---

### Task A5: Run the delay tests against all three resources

**Files:**
- Modify: `internal/provider/shared_test.go`, `internal/provider/resource_list_test.go`, `internal/provider/resource_set_test.go`, `internal/provider/resource_map_test.go`

**Interfaces:**
- Consumes: everything Task A4 produced, plus `pendingDetail`, `pendingLeaverDetail`, `expectPlanDetailVisibleUnknown`.
- Produces: `var delayCases []sharedCase`; `sharedGroups` now `[][]sharedCase{expiryCases, delayCases}`.

- [ ] **Step 1: Register the delay group**

In `internal/provider/shared_test.go`, replace

```go
var sharedGroups = [][]sharedCase{expiryCases}
```

with

```go
var sharedGroups = [][]sharedCase{expiryCases, delayCases}
```

- [ ] **Step 2: Append the delay rows at the end of shared_test.go**

```go
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
```

- [ ] **Step 3: Run the new rows before deleting anything**

Run: `make testacc TESTARGS='-run "TestAccShared/(CreateWithDelayedFor|OutputsKnownAtPlanTimeWithDelayedFor|RevealRealizedOnRefresh|DelayedFor|PendingLeaverStamped|ResetDelaysSeed|UnknownDelayedForAtPlan|ImportThenDelayedFor)"'`
Expected: 36 leaf subtests pass (12 rows × 3 flavors); final line `ok`.

- [ ] **Step 4: Delete the migrated delay tests**

From `internal/provider/resource_list_test.go` delete, each with its doc comment: `TestAccListDelayedForRemoved`, `TestAccListDelayedForZeroRevealsNextRefresh`, `TestAccListDelayedForMustBeShorterThanExpiresAfter`, `TestAccListPendingLeaverStamped`, `TestAccListUnknownDelayedForAtPlan` (5).

From `internal/provider/resource_set_test.go` delete, each with its doc comment: `TestAccSetCreateWithDelayedFor`, `TestAccSetOutputsKnownAtPlanTimeWithDelayedFor`, `TestAccSetRevealRealizedOnRefresh`, `TestAccSetDelayedForRemoved`, `TestAccSetDelayedForDecreaseRevealsSooner`, `TestAccSetDelayedForIncreaseKeepsPending`, `TestAccSetDelayedForZeroRevealsNextRefresh`, `TestAccSetDelayedForMustBeShorterThanExpiresAfter`, `TestAccSetPendingLeaverStamped`, `TestAccSetResetDelaysSeed`, `TestAccSetUnknownDelayedForAtPlan`, `TestAccSetImportThenDelayedFor` (12), and the helper `expectSetPlanAccumulated`, which nothing uses any more.

From `internal/provider/resource_map_test.go` delete, each with its doc comment: `TestAccMapCreateWithDelayedFor`, `TestAccMapEnteringKeyHidden`, `TestAccMapRevealRealizedOnRefresh`, `TestAccMapDelayedForRemoved`, `TestAccMapDelayedForZeroRevealsNextRefresh`, `TestAccMapDelayedForMustBeShorterThanExpiresAfter`, `TestAccMapPendingLeaverStamped`, `TestAccMapResetDelaysSeed`, `TestAccMapUnknownDelayedForAtPlan`, `TestAccMapImportThenDelayedFor`, `TestAccMapDelayedForDecreaseRevealsSooner`, `TestAccMapDelayedForIncreaseKeepsPending` (12).

- [ ] **Step 5: Drop regexp from the three resource test files**

Each of `resource_list_test.go`, `resource_set_test.go`, and `resource_map_test.go` now has exactly this import block:

```go
import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)
```

- [ ] **Step 6: Build, count, and run the full suite**

Run: `gofmt -l . ; go vet ./internal/provider/ && go test ./internal/... && grep -c '^func TestAcc' internal/provider/*_test.go`
Expected: no `gofmt` output; `ok` for both packages; counts `import_test.go:8`, `resource_list_test.go:22`, `resource_map_test.go:13`, `resource_set_test.go:10`, `shared_test.go:1` (54 in total).

Run: `make testacc 2>&1 | tee /tmp/accumulator-a5.log | grep -E '^(--- FAIL|ok|FAIL)'; grep -cE '^        --- PASS: TestAccShared/' /tmp/accumulator-a5.log; grep -c '^--- PASS: TestAcc' /tmp/accumulator-a5.log`
Expected: no `--- FAIL`; `ok`; `84` leaf matrix subtests; `54` top-level acceptance tests.

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/shared_test.go internal/provider/resource_list_test.go internal/provider/resource_set_test.go internal/provider/resource_map_test.go
git commit -m "test: run delay tests against all three resources"
```

---

# Part B: shared resource core

Part B starts from the tip of Part A. Its safety nets, in order of strength: `TestPhasesGolden` (byte-for-byte plan, apply, read, import, and validate output for 114 scenario × resource sections, Task B1), the 54 acceptance tests and 84 matrix subtests from Part A, the provider schema export compared byte for byte (Task B7), and the state-shape unit tests.

Every task below replaces whole declarations. "Replace `X`" means: delete the declaration named `X` together with its doc comment, and put the given code in its place. Method bodies not named in a task stay as they are.

### Task B1: Pin every phase's output with a golden file

**Files:**
- Create: `internal/provider/golden_test.go`
- Create (generated): `internal/provider/testdata/phases.golden`

**Interfaces:**
- Consumes: `listResource`, `setResource`, `mapResource` (their `ModifyPlan`, `Create`, `Update`, `Read`, `ImportState`, `ValidateConfig`), `resourceSchema` (`model_test.go`).
- Produces: `func TestPhasesGolden(t *testing.T)`; the `-update` test flag; `testdata/phases.golden`.

- [ ] **Step 1: Record the acceptance baseline at the pre-refactor commit**

Run, before changing anything:

```bash
mkdir -p /tmp/accumulator-core
git rev-parse HEAD > /tmp/accumulator-core/before.rev
make testacc TESTARGS='-count=1' > /tmp/accumulator-core/acc-before.log 2>&1; echo "exit $?"
grep -E '^\s*--- (PASS|FAIL|SKIP): TestAcc' /tmp/accumulator-core/acc-before.log | sed 's/ (.*//' | sort > /tmp/accumulator-core/acc-before.txt
wc -l < /tmp/accumulator-core/acc-before.txt
./tools/gen-schema.sh && cp tools/schema.json /tmp/accumulator-core/schema-before.json
```

Expected: `exit 0`; `176` lines (54 top-level tests, 28 matrix rows, 84 row × flavor subtests, 10 malformed-import subtests). `tools/schema.json` is gitignored, so nothing in the tree changes.

- [ ] **Step 2: Write the golden test**

Create `internal/provider/golden_test.go`:

```go
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
```

- [ ] **Step 3: Generate the golden file from the current code**

Run: `go test ./internal/provider/ -run TestPhasesGolden -update && wc -l internal/provider/testdata/phases.golden && grep -c '^==' internal/provider/testdata/phases.golden`
Expected: `ok`; `341` lines; `114` sections (3 resources × (24 plan + 5 read + 5 import + 4 validate)).

Spot-check three lines, which pin the behaviors the refactor must keep:

```bash
grep -A1 '^== map / plan / expires_after decrease' internal/provider/testdata/phases.golden | tail -1 | grep -o '"b": {"expires_at": <unknown>'
grep -A1 '^== set / import / seed' internal/provider/testdata/phases.golden | tail -1 | grep -o '"id": "0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13"'
grep -c 'diag Error \[delayed_for\] delayed_for must be shorter than expires_after' internal/provider/testdata/phases.golden
```

Expected: `"b": {"expires_at": <unknown>`; the set import id (the hash of the deduplicated `["a","b"]`); `9` (three per resource: the plan case and the validate equal and greater cases).

- [ ] **Step 4: Confirm the golden is stable**

Run: `go test ./internal/provider/ -run TestPhasesGolden -count=5`
Expected: `ok`. Clock-derived timestamps print as `<clock>`, so the file does not depend on when it runs.

- [ ] **Step 5: Prove the golden catches the review's wiring mistake**

```bash
sed -i '0,/args.PriorExpires = int64Ptr(state.ExpiresAfter)/s//args.PriorExpires = int64Ptr(plan.ExpiresAfter)/' internal/provider/resource_map.go
go test ./internal/provider/ -run TestPhasesGolden 2>&1 | head -4
git checkout -- internal/provider/resource_map.go
```

Expected before the checkout: `--- FAIL: TestPhasesGolden` with `testdata/phases.golden differs at line 260:`, where `got` has `"b": {"expires_at": "2999-01-01T00:00:00Z"` and `want` has `"b": {"expires_at": <unknown>`. After the checkout, `go test ./internal/provider/ -run TestPhasesGolden` prints `ok`.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/golden_test.go internal/provider/testdata/phases.golden
git commit -m "test: pin phase outputs with a golden file"
```

---

### Task B2: Embed the shared attributes in coreModel

**Files:**
- Create: `internal/provider/core.go`, `internal/provider/core_test.go`
- Modify: `internal/provider/resource_list.go`, `internal/provider/resource_set.go`, `internal/provider/resource_map.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `type coreModel struct { TriggersReset, TriggersReplacement types.String; ExpiresAfter, DelayedFor types.Int64; DetailedOutputs types.Map; ID types.String }` (tags unchanged)
  - `func importedCore(detailed types.Map, id string) coreModel`
  - `listResourceModel`, `setResourceModel`, `mapResourceModel` each embed `coreModel` and keep only `Inputs`, `Accumulated`, `Outputs` (and the list's `Length`).

- [ ] **Step 1: Write the round-trip test against the flat models**

Create `internal/provider/core_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestModelsRoundTripThroughEmbeddedCore pins that embedding coreModel
// leaves every model's state shape unchanged: a fully populated raw state
// decodes into the model and encodes back to an identical raw value, so
// every tfsdk tag still resolves, promoted fields included.
func TestModelsRoundTripThroughEmbeddedCore(t *testing.T) {
	ctx := context.Background()
	stringType := tftypes.String
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	detailed := tftypes.NewValue(tftypes.Map{ElementType: detailType}, map[string]tftypes.Value{
		"a": tftypes.NewValue(detailType, map[string]tftypes.Value{
			"expires_at": tftypes.NewValue(stringType, "2000-01-01T00:00:00Z"),
			"visible_at": tftypes.NewValue(stringType, nil),
		}),
	})
	shared := map[string]tftypes.Value{
		"triggers_reset":       tftypes.NewValue(stringType, "r"),
		"triggers_replacement": tftypes.NewValue(stringType, "p"),
		"expires_after":        tftypes.NewValue(tftypes.Number, 3600),
		"delayed_for":          tftypes.NewValue(tftypes.Number, 60),
		"detailed_outputs":     detailed,
		"id":                   tftypes.NewValue(stringType, "x"),
	}
	list := tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")})
	set := tftypes.NewValue(tftypes.Set{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")})
	strMap := tftypes.NewValue(tftypes.Map{ElementType: stringType}, map[string]tftypes.Value{"a": tftypes.NewValue(stringType, "1")})

	for label, tc := range map[string]struct {
		res   resource.Resource
		own   map[string]tftypes.Value
		model func() any
	}{
		"list": {&listResource{}, map[string]tftypes.Value{
			"inputs": list, "length": tftypes.NewValue(tftypes.Number, 5), "accumulated": list, "outputs": list,
		}, func() any { return &listResourceModel{} }},
		"set": {&setResource{}, map[string]tftypes.Value{
			"inputs": list, "accumulated": set, "outputs": set,
		}, func() any { return &setResourceModel{} }},
		"map": {&mapResource{}, map[string]tftypes.Value{
			"inputs": strMap, "accumulated": strMap, "outputs": strMap,
		}, func() any { return &mapResourceModel{} }},
	} {
		t.Run(label, func(t *testing.T) {
			sch := resourceSchema(t, tc.res)
			attrs := map[string]tftypes.Value{}
			for k, v := range shared {
				attrs[k] = v
			}
			for k, v := range tc.own {
				attrs[k] = v
			}
			raw := tftypes.NewValue(sch.Type().TerraformType(ctx), attrs)
			model := tc.model()
			if diags := (tfsdk.State{Raw: raw, Schema: sch}).Get(ctx, model); diags.HasError() {
				t.Fatalf("Get: %+v", diags)
			}
			out := tfsdk.State{Raw: tftypes.NewValue(sch.Type().TerraformType(ctx), nil), Schema: sch}
			if diags := out.Set(ctx, model); diags.HasError() {
				t.Fatalf("Set: %+v", diags)
			}
			if !out.Raw.Equal(raw) {
				t.Fatalf("round trip changed the state:\n got: %s\nwant: %s", out.Raw, raw)
			}
		})
	}
}
```

Run: `go test ./internal/provider/ -run TestModelsRoundTripThroughEmbeddedCore -v`
Expected: `--- PASS: TestModelsRoundTripThroughEmbeddedCore` with `list`, `set`, `map` subtests. It passes on the flat models; it must still pass after Step 3.

- [ ] **Step 2: Create core.go with coreModel**

```go
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
```

- [ ] **Step 3: Embed coreModel in the three models**

In `internal/provider/resource_list.go`, replace `listResourceModel` with:

```go
// listResourceModel is accumulator_list's state model: the shared
// attributes plus length and the three lists.
type listResourceModel struct {
	coreModel
	Inputs      types.List  `tfsdk:"inputs"`
	Length      types.Int64 `tfsdk:"length"`
	Accumulated types.List  `tfsdk:"accumulated"`
	Outputs     types.List  `tfsdk:"outputs"`
}
```

In `internal/provider/resource_set.go`, replace `setResourceModel` with:

```go
// setResourceModel is accumulator_set's state model: the shared attributes
// plus inputs (a list, the shape users write) and the two sets.
type setResourceModel struct {
	coreModel
	Inputs      types.List `tfsdk:"inputs"`
	Accumulated types.Set  `tfsdk:"accumulated"`
	Outputs     types.Set  `tfsdk:"outputs"`
}
```

In `internal/provider/resource_map.go`, replace `mapResourceModel` with:

```go
// mapResourceModel is accumulator_map's state model: the shared attributes
// plus the three maps.
type mapResourceModel struct {
	coreModel
	Inputs      types.Map `tfsdk:"inputs"`
	Accumulated types.Map `tfsdk:"accumulated"`
	Outputs     types.Map `tfsdk:"outputs"`
}
```

- [ ] **Step 4: Build the imported models through importedCore**

The flat struct literals no longer compile. Replace `listResource.ImportState` with:

```go
// ImportState seeds the resource from a JSON object. length, triggers_reset,
// and triggers_replacement are left null: length is required configuration, so
// the next plan fills it in and Update re-trims the seeded outputs. Seeding
// inputs is what lets a matching configuration produce an empty plan instead of
// appending the configured list a second time.
func (r *listResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseImport(req.ID)
	if err != nil {
		// The error names the expected shape and quotes the offending key only.
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := listFromStrings(ctx, seed.Inputs)
	resp.Diagnostics.Append(d...)
	outputs, d := listFromStrings(ctx, seed.Outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	detailed, d := mapFromStamps(ctx, seed.Outputs, stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &listResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashID(seed.Inputs)),
		Inputs:      inputs,
		Length:      types.Int64Null(),
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
```

Replace `setResource.ImportState` with:

```go
// ImportState seeds the set from a JSON object. outputs is deduplicated and
// id hashes the seeded outputs. inputs is seeded as an empty list rather than
// null: it is a required attribute, and an empty list is a valid non-null
// value, so nothing depends on tolerating a null required attribute in
// imported state. The next plan sets inputs from configuration and Update
// unions it into the seeded outputs. An inputs key in the ID is accepted and
// ignored.
func (r *setResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseImport(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := listFromStrings(ctx, []string{})
	resp.Diagnostics.Append(d...)
	// Hash the deduplicated outputs, not the raw seed: a seed such as
	// {"outputs":["a","a","b"]} then yields the same id as
	// {"outputs":["a","b"]}, matching the state it produces
	// (2026-09-17-terraform-provider-accumulator-design.md section 8).
	merged := accumulate.Union(nil, seed.Outputs)
	outputs, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	detailed, d := mapFromStamps(ctx, merged, stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &setResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashID(merged)),
		Inputs:      inputs,
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
```

Replace `mapResource.ImportState` with:

```go
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
	// ParseMapImport returns a fresh, non-nil map, so no defensive copy is needed.
	seeded := seed.Outputs
	outputs, d := mapFromStringMap(ctx, seeded)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, accumulate.SortedKeys(seeded), stamps{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &mapResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashMapID(seeded)),
		Inputs:      inputs,
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
```

Every other method reads and writes the same fields through promotion and needs no change.

- [ ] **Step 5: Prove state compatibility and determinism**

Run: `go vet ./internal/provider/ && go test ./internal/provider/ -run 'TestModelsRoundTrip|TestProviderSchema|TestStateGetToleratesMissingDelayAttributes|ReadFillsNullAccumulated|TestPhasesGolden' -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected:

```
--- PASS: TestModelsRoundTripThroughEmbeddedCore
--- PASS: TestPhasesGolden
--- PASS: TestStateGetToleratesMissingDelayAttributes
--- PASS: TestSetReadFillsNullAccumulated
--- PASS: TestMapReadFillsNullAccumulated
--- PASS: TestListReadFillsNullAccumulated
--- PASS: TestProviderSchema
ok
```

Then run the whole unit suite: `go test ./internal/...` → `ok` for both packages.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/core.go internal/provider/core_test.go internal/provider/resource_list.go internal/provider/resource_set.go internal/provider/resource_map.go
git commit -m "refactor: embed the shared attributes in coreModel"
```

---

### Task B3: Share Read through a collection shape

**Files:**
- Modify: `internal/provider/core.go`, `internal/provider/resource_list.go`, `internal/provider/resource_set.go`, `internal/provider/resource_map.go`

**Interfaces:**
- Consumes: `stringsFromList`, `stringsFromSet`, `stringMapFromMap`, `listFromStrings`, `setFromStrings`, `mapFromStringMap`, `stampsFromMap`, `mapFromStamps`, `clockNow`, `accumulate.FilterExpired`, `accumulate.FilterPending`, `accumulate.SortedKeys`, `accumulate.Project`, `accumulate.HashID`, `accumulate.HashMapID`, `accumulate.ParseImport`, `accumulate.ParseMapImport`, `accumulate.Union`.
- Produces:
  - `type shape[C any, V attr.Value] struct { decode func(context.Context, V) (C, diag.Diagnostics); encode func(context.Context, C) (V, diag.Diagnostics); keys func(C) []string; project func(c C, keys []string) C; hash func(C) string; parse func(id string) (inputs, outputs C, err error) }`
  - `func sliceShape[V attr.Value](decode func(context.Context, V) ([]string, diag.Diagnostics), encode func(context.Context, []string) (V, diag.Diagnostics), parse func(id string) (inputs, outputs []string, err error)) shape[[]string, V]`
  - `func priorAccumulated[C any, V attr.Value](ctx context.Context, s shape[C, V], accumulated, outputs V) (C, diag.Diagnostics)`
  - `func readPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], m *coreModel, storedAccumulated, storedOutputs V) (accumulated, outputs V, changed bool, diags diag.Diagnostics)`
  - `var listShape shape[[]string, types.List]`, `var setShape shape[[]string, types.Set]`, `var mapShape shape[map[string]string, types.Map]`

The package-level `priorAccumulated` coexists with the three `priorAccumulated` methods until Task B5 removes them; a method and a package function may share a name.

- [ ] **Step 1: Widen core.go's imports**

Replace the import block of `internal/provider/core.go` with:

```go
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)
```

- [ ] **Step 2: Append the shape and readPhase to core.go**

Append after `importedCore`:

```go
// shape adapts one collection type to the shared phases. C is the Go value
// the pure functions in internal/accumulate take ([]string or
// map[string]string); V is the framework type accumulated and outputs are
// stored as (types.List, types.Set, or types.Map). Each resource declares
// one shape value; nothing in it reads or writes a model.
type shape[C any, V attr.Value] struct {
	// decode reads accumulated or outputs. Null and unknown read as empty.
	decode func(context.Context, V) (C, diag.Diagnostics)
	// encode writes a collection as a concrete, never-null V.
	encode func(context.Context, C) (V, diag.Diagnostics)
	// keys is the identity list the stamp functions run over: the values
	// themselves for a list or set, the sorted keys for a map.
	keys func(C) []string
	// project maps a key list returned by a stamp function back onto c:
	// the keys themselves for a list or set, the entries of c with those
	// keys for a map.
	project func(c C, keys []string) C
	// hash is the id of a collection: accumulate.HashID or HashMapID.
	hash func(C) string
	// parse reads an import ID into the seeded inputs and outputs. A
	// resource that seeds no inputs returns nil for them.
	parse func(id string) (inputs, outputs C, err error)
}

// sliceShape fills the parts of a shape that are the same for the list and
// the set: a []string is its own identity list.
func sliceShape[V attr.Value](decode func(context.Context, V) ([]string, diag.Diagnostics),
	encode func(context.Context, []string) (V, diag.Diagnostics),
	parse func(id string) (inputs, outputs []string, err error)) shape[[]string, V] {
	return shape[[]string, V]{
		decode:  decode,
		encode:  encode,
		keys:    func(values []string) []string { return values },
		project: func(_ []string, keys []string) []string { return keys },
		hash:    accumulate.HashID,
		parse:   parse,
	}
}

// priorAccumulated reads the store from state: accumulated, or outputs when
// state predates the attribute (2026-10-15-accumulator-delayed-for-design.md
// section 9, 2026-10-18-accumulator-map-design.md section 10).
func priorAccumulated[C any, V attr.Value](ctx context.Context, s shape[C, V], accumulated, outputs V) (C, diag.Diagnostics) {
	if accumulated.IsNull() {
		return s.decode(ctx, outputs)
	}
	return s.decode(ctx, accumulated)
}

// readPhase is Read's shared body: it culls expired values and reveals due
// ones, the refresh-time half of both designs. Read cannot see
// configuration, so both decisions run on stored stamps alone. changed is
// false when nothing was dropped or revealed and state already holds
// accumulated; the caller then writes nothing, which keeps the plan after a
// refresh empty. On change it rewrites m.DetailedOutputs and returns the new
// collections.
func readPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], m *coreModel, storedAccumulated, storedOutputs V) (accumulated, outputs V, changed bool, diags diag.Diagnostics) {
	values, d := priorAccumulated(ctx, s, storedAccumulated, storedOutputs)
	diags.Append(d...)
	prior, d := stampsFromMap(ctx, m.DetailedOutputs)
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	keys := s.keys(values)
	now := clockNow()
	survivors, expires := accumulate.FilterExpired(keys, prior.Expires, now)
	visible, pending := accumulate.FilterPending(survivors, prior.Visible, now)
	if !storedAccumulated.IsNull() && len(survivors) == len(keys) && len(pending) == len(prior.Visible) {
		return accumulated, outputs, false, diags
	}

	accumulated, d = s.encode(ctx, s.project(values, survivors))
	diags.Append(d...)
	outputs, d = s.encode(ctx, s.project(values, visible))
	diags.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps{Expires: expires, Visible: pending})
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}
	m.DetailedOutputs = detailed
	return accumulated, outputs, true, diags
}
```

- [ ] **Step 3: Declare each resource's shape after its constructor**

In `internal/provider/resource_list.go`, insert after `NewListResource`:

```go
// listShape adapts accumulator_list to the shared phases in core.go: a list
// of strings is its own identity list, duplicates and order included.
// Import seeds both inputs and outputs from the ID.
var listShape = sliceShape(stringsFromList, listFromStrings,
	func(id string) ([]string, []string, error) {
		seed, err := accumulate.ParseImport(id)
		return seed.Inputs, seed.Outputs, err
	})
```

In `internal/provider/resource_set.go`, insert after `NewSetResource`:

```go
// setShape adapts accumulator_set to the shared phases in core.go: a set of
// strings is its own identity list. Import deduplicates the seeded outputs
// and seeds no inputs; an inputs key in the ID is accepted and ignored.
var setShape = sliceShape(stringsFromSet, setFromStrings,
	func(id string) ([]string, []string, error) {
		seed, err := accumulate.ParseImport(id)
		if err != nil {
			return nil, nil, err
		}
		return nil, accumulate.Union(nil, seed.Outputs), nil
	})
```

In `internal/provider/resource_map.go`, insert after `NewMapResource`:

```go
// mapShape adapts accumulator_map to the shared phases in core.go. The stamp
// functions run over the sorted keys, so values never reach them
// (2026-10-18-accumulator-map-design.md V3: a value change is never an
// entry), and Project re-attaches the values. Import seeds no inputs; an
// inputs key in the ID is accepted and ignored.
var mapShape = shape[map[string]string, types.Map]{
	decode:  stringMapFromMap,
	encode:  mapFromStringMap,
	keys:    accumulate.SortedKeys,
	project: accumulate.Project,
	hash:    accumulate.HashMapID,
	parse: func(id string) (map[string]string, map[string]string, error) {
		seed, err := accumulate.ParseMapImport(id)
		return nil, seed.Outputs, err
	},
}
```

- [ ] **Step 4: Route Read through readPhase**

Replace `listResource.Read` with:

```go
// Read culls expired values and reveals due ones through readPhase.
// Duplicates share a stamp, so they expire and reveal together. It writes
// state only when a value was dropped or revealed, or when state predates
// accumulated and needs it filled from outputs.
func (r *listResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state listResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	acc, out, changed, d := readPhase(ctx, listShape, &state.coreModel, state.Accumulated, state.Outputs)
	resp.Diagnostics.Append(d...)
	if !changed || resp.Diagnostics.HasError() {
		return
	}
	state.Accumulated, state.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
```

Replace `setResource.Read` with:

```go
// Read culls expired values and reveals due ones through readPhase. It
// writes state only when a value was dropped or revealed, or when state
// predates accumulated and needs it filled from outputs.
func (r *setResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state setResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	acc, out, changed, d := readPhase(ctx, setShape, &state.coreModel, state.Accumulated, state.Outputs)
	resp.Diagnostics.Append(d...)
	if !changed || resp.Diagnostics.HasError() {
		return
	}
	state.Accumulated, state.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
```

Replace `mapResource.Read` with:

```go
// Read culls expired keys and reveals due ones through readPhase. It writes
// state only when a key was dropped or revealed, or when state lacks
// accumulated and needs it filled from outputs.
func (r *mapResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mapResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	acc, out, changed, d := readPhase(ctx, mapShape, &state.coreModel, state.Accumulated, state.Outputs)
	resp.Diagnostics.Append(d...)
	if !changed || resp.Diagnostics.HasError() {
		return
	}
	state.Accumulated, state.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
```

For the list and set, `keys` and `project` return their argument, so `readPhase` calls `FilterExpired(values, …)` and encodes `survivors` and `visible` exactly as the old bodies did; for the map it calls them on `SortedKeys(values)` and projects back, as before.

- [ ] **Step 5: Run the unit suite**

Run: `gofmt -l . ; go vet ./... && go test ./internal/... -count=1`
Expected: no `gofmt` output; `ok` for both packages (`TestPhasesGolden`'s read sections, including "accumulated null" and "corrupt detailed_outputs", are unchanged).

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/core.go internal/provider/resource_list.go internal/provider/resource_set.go internal/provider/resource_map.go
git commit -m "refactor: share Read through a collection shape"
```

---

### Task B4: Share ModifyPlan through planPhase

**Files:**
- Modify: `internal/provider/core.go`, `internal/provider/resource_list.go`, `internal/provider/resource_set.go`, `internal/provider/resource_map.go`

**Interfaces:**
- Consumes: `checkDelayShorterThanExpiry`, `planStamps`, `stampArgs`, `int64Ptr`, `listContainsUnknown`, `mapContainsUnknown`, `accumulate.NextListOutputs`, `accumulate.NextSetOutputs`, `accumulate.NextMapOutputs`.
- Produces:
  - `func (c coreModel) configUnknown() bool`, `func (c coreModel) stateUnknown() bool`
  - `type nextFunc[C any] func(reseed bool, prior C) C`
  - `func newStampArgs(planInputs []string, plan, state *coreModel, priorKeys []string, prior stamps, reseed bool) stampArgs`
  - `func readPlanAndState[M any](ctx context.Context, req resource.ModifyPlanRequest, plan, state *M, diags *diag.Diagnostics) (creating, ok bool)`
  - `type planCall[C any, V attr.Value] struct { creating bool; plan, state *coreModel; stateAccumulated, stateOutputs V; planInputs C; next nextFunc[C] }`
  - `func planPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], call planCall[C, V]) (accumulated, outputs V, ok bool, diags diag.Diagnostics)`
  - `func listNext(planInputs []string, inputsChanged bool, length int) nextFunc[[]string]`, `func setNext(planInputs []string) nextFunc[[]string]`, `func mapNext(planInputs map[string]string) nextFunc[map[string]string]`

The guard split: each resource checks its own terms (`inputs` elements, the list's `length`, and `state.Inputs`), and `planPhase` checks the shared ones (both triggers, `expires_after`, `delayed_for`, state `detailed_outputs`, state `accumulated` and `outputs`). The union is the old 5-term and 8-term guard for every resource. The list's guard still does not check `state.Length`, as before (review section 1); adding it is a behavior question for another change.

- [ ] **Step 1: Add resource to core.go's imports**

Replace the import block of `internal/provider/core.go` with:

```go
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)
```

- [ ] **Step 2: Append the plan phase to core.go**

Append after `readPhase`:

```go
// configUnknown reports whether a shared branch input is unknown in a plan:
// a trigger, expires_after, or delayed_for. Each resource adds its own
// terms (inputs, and the list's length).
func (c coreModel) configUnknown() bool {
	return c.TriggersReset.IsUnknown() || c.TriggersReplacement.IsUnknown() ||
		c.ExpiresAfter.IsUnknown() || c.DelayedFor.IsUnknown()
}

// stateUnknown is configUnknown for prior state, which also mirrors the
// computed detailed_outputs. State never holds unknowns in practice; the
// guard keeps ModifyPlan from computing against one if it ever did.
func (c coreModel) stateUnknown() bool {
	return c.configUnknown() || c.DetailedOutputs.IsUnknown()
}

// nextFunc is a resource's accumulation step: the next accumulated value
// from the prior one, with the planned inputs already bound. reseed
// discards prior (a create, a replacement, or a triggers_reset change).
type nextFunc[C any] func(reseed bool, prior C) C

// newStampArgs builds the stamp decision inputs for planInputs. state is
// nil on create, where nothing is prior. A reseed keeps the prior
// expires_after and delayed_for (they still decide whether a stamp is
// recalculated) but discards the prior store and stamps, so every value
// enters with a full delay window
// (2026-10-15-accumulator-delayed-for-design.md T4).
func newStampArgs(planInputs []string, plan, state *coreModel, priorKeys []string, prior stamps, reseed bool) stampArgs {
	args := stampArgs{
		PlanInputs:     planInputs,
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if state != nil {
		args.PriorExpires = int64Ptr(state.ExpiresAfter)
		args.PriorDelay = int64Ptr(state.DelayedFor)
	}
	if !reseed {
		args.PriorAccumulated = priorKeys
		args.Prior = prior
	}
	return args
}

// readPlanAndState is ModifyPlan's opening, shared by every resource. It
// decodes the plan, and the state unless the resource is being created
// (state is null then, and Get refuses a null state). ok is false on a
// destroy plan, which carries no planned state to compute from and must
// stay null, and on a decode error.
func readPlanAndState[M any](ctx context.Context, req resource.ModifyPlanRequest, plan, state *M, diags *diag.Diagnostics) (creating, ok bool) {
	if req.Plan.Raw.IsNull() {
		return false, false
	}
	diags.Append(req.Plan.Get(ctx, plan)...)
	if diags.HasError() {
		return false, false
	}
	creating = req.State.Raw.IsNull()
	if !creating {
		diags.Append(req.State.Get(ctx, state)...)
		if diags.HasError() {
			return creating, false
		}
	}
	return creating, true
}

// planCall is one ModifyPlan call's per-resource input to planPhase.
type planCall[C any, V attr.Value] struct {
	// creating is true when there is no prior state.
	creating bool
	// plan is the planned shared attributes; planPhase writes its
	// detailed_outputs and, on create or replacement, its id.
	plan *coreModel
	// state is the prior shared attributes; ignored when creating.
	state *coreModel
	// stateAccumulated and stateOutputs are the prior collections.
	stateAccumulated, stateOutputs V
	// planInputs is the decoded planned inputs.
	planInputs C
	// next computes the next accumulated value.
	next nextFunc[C]
}

// planPhase is ModifyPlan's shared body. It writes the outputs the next apply
// will produce into the plan, so the plan shows concrete values and
// downstream resources can plan against them. It runs the same
// accumulation (call.next) as Create and Update, so the planned value cannot
// disagree with the applied one. It reads no clock
// (2026-10-15-accumulator-delayed-for-design.md section 6): fresh
// detailed_outputs entries plan as unknown and the apply stamps them.
//
// ok is false when the plan must be left as the framework built it: an
// unknown branch input makes the planned outputs unknowable, not merely
// unresolved, because a triggers_reset that may have changed yields either
// the reseeded collection or the accumulated one, and no single planned
// value can express that. Leaving outputs untouched keeps the framework's
// unknown marking in place until Create or Update resolves the value. The
// caller has already checked its own terms (inputs, and the list's length).
// ok is also false on an error.
func planPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], call planCall[C, V]) (accumulated, outputs V, ok bool, diags diag.Diagnostics) {
	if call.plan.configUnknown() {
		return accumulated, outputs, false, diags
	}
	if !call.creating && (call.state.stateUnknown() ||
		call.stateAccumulated.IsUnknown() || call.stateOutputs.IsUnknown()) {
		return accumulated, outputs, false, diags
	}
	diags.Append(checkDelayShorterThanExpiry(call.plan.DelayedFor, call.plan.ExpiresAfter)...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	var prior C
	var priorStamps stamps
	var state *coreModel
	if !call.creating {
		state = call.state
		var d diag.Diagnostics
		prior, d = priorAccumulated(ctx, s, call.stateAccumulated, call.stateOutputs)
		diags.Append(d...)
		priorStamps, d = stampsFromMap(ctx, call.state.DetailedOutputs)
		diags.Append(d...)
	}
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	// The branch conditions mirror Update's, with two additions: a null state
	// means Create runs, and a triggers_replacement change means a
	// replacement, which also runs Create. Both seed from the planned inputs
	// alone, which is what reseed expresses.
	replacing := !call.creating && !call.plan.TriggersReplacement.Equal(call.state.TriggersReplacement)
	reseed := call.creating || replacing || !call.plan.TriggersReset.Equal(call.state.TriggersReset)

	merged := call.next(reseed, prior)
	var priorKeys []string
	if !reseed {
		priorKeys = s.keys(prior)
	}
	args := newStampArgs(s.keys(call.planInputs), call.plan, state, priorKeys, priorStamps, reseed)

	// No clock here (2026-10-15-accumulator-delayed-for-design.md section 6):
	// refresh already culled and revealed, so membership is a pure function of
	// state and configuration.
	visible, detailed, d := planStamps(ctx, s.keys(merged), args)
	diags.Append(d...)
	accumulated, d = s.encode(ctx, merged)
	diags.Append(d...)
	outputs, d = s.encode(ctx, s.project(merged, visible))
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, false, diags
	}

	call.plan.DetailedOutputs = detailed
	if call.creating || replacing {
		// Create identifies the resource from the planned inputs. An in-place
		// update keeps the state id; UseStateForUnknown has already resolved
		// it into the plan by the time ModifyPlan runs.
		call.plan.ID = types.StringValue(s.hash(call.planInputs))
	}
	return accumulated, outputs, true, diags
}
```

- [ ] **Step 3: Declare each resource's accumulation step after its shape**

In `internal/provider/resource_list.go`, insert after `listShape`:

```go
// listNext is accumulator_list's accumulation step. Append is not
// idempotent, so unlike the set and map the list compares planned inputs
// against the previous apply's (inputsChanged), and it trims to length.
func listNext(planInputs []string, inputsChanged bool, length int) nextFunc[[]string] {
	return func(reseed bool, prior []string) []string {
		return accumulate.NextListOutputs(reseed, inputsChanged, planInputs, prior, length)
	}
}
```

In `internal/provider/resource_set.go`, insert after `setShape`:

```go
// setNext is accumulator_set's accumulation step: a union of the planned
// inputs into the prior set. Union is idempotent, so no inputs comparison is
// needed: unioning whenever Create or Update runs is correct.
func setNext(planInputs []string) nextFunc[[]string] {
	return func(reseed bool, prior []string) []string {
		return accumulate.NextSetOutputs(reseed, planInputs, prior)
	}
}
```

In `internal/provider/resource_map.go`, insert after `mapShape`:

```go
// mapNext is accumulator_map's accumulation step: the planned inputs merged
// over the prior map. Merging is idempotent, so no inputs comparison is
// needed: merging whenever Create or Update runs is correct.
func mapNext(planInputs map[string]string) nextFunc[map[string]string] {
	return func(reseed bool, prior map[string]string) map[string]string {
		return accumulate.NextMapOutputs(reseed, planInputs, prior)
	}
}
```

- [ ] **Step 4: Route ModifyPlan through planPhase**

Replace `listResource.ModifyPlan` with:

```go
// ModifyPlan writes the outputs the next apply will produce into the plan
// through planPhase. length is a branch input of its own, and Terraform
// marks unknown list elements individually, so an unknown element counts
// too.
func (r *listResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan, state listResourceModel
	creating, ok := readPlanAndState(ctx, req, &plan, &state, &resp.Diagnostics)
	if !ok || listContainsUnknown(plan.Inputs) || plan.Length.IsUnknown() ||
		(!creating && state.Inputs.IsUnknown()) {
		return
	}
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	inputsChanged := !creating && !plan.Inputs.Equal(state.Inputs)

	acc, out, ok, d := planPhase(ctx, listShape, planCall[[]string, types.List]{
		creating:         creating,
		plan:             &plan.coreModel,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             listNext(planInputs, inputsChanged, int(plan.Length.ValueInt64())),
	})
	resp.Diagnostics.Append(d...)
	if !ok || resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}
```

Replace `setResource.ModifyPlan` with:

```go
// ModifyPlan writes the outputs the next apply will produce into the plan
// through planPhase. inputs is a list, and Terraform marks unknown list
// elements individually, so an unknown element counts as an unknown branch
// input.
func (r *setResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan, state setResourceModel
	creating, ok := readPlanAndState(ctx, req, &plan, &state, &resp.Diagnostics)
	if !ok || listContainsUnknown(plan.Inputs) || (!creating && state.Inputs.IsUnknown()) {
		return
	}
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)

	acc, out, ok, d := planPhase(ctx, setShape, planCall[[]string, types.Set]{
		creating:         creating,
		plan:             &plan.coreModel,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             setNext(planInputs),
	})
	resp.Diagnostics.Append(d...)
	if !ok || resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}
```

Replace `mapResource.ModifyPlan` with:

```go
// ModifyPlan writes the outputs the next apply will produce into the plan
// through planPhase. Terraform marks unknown map values individually, so an
// unknown value counts as an unknown branch input; a map whose keys are
// unknown is wholly unknown.
func (r *mapResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan, state mapResourceModel
	creating, ok := readPlanAndState(ctx, req, &plan, &state, &resp.Diagnostics)
	if !ok || mapContainsUnknown(plan.Inputs) || (!creating && state.Inputs.IsUnknown()) {
		return
	}
	planInputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)

	acc, out, ok, d := planPhase(ctx, mapShape, planCall[map[string]string, types.Map]{
		creating:         creating,
		plan:             &plan.coreModel,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             mapNext(planInputs),
	})
	resp.Diagnostics.Append(d...)
	if !ok || resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}
```

- [ ] **Step 5: Run the unit suite**

Run: `gofmt -l . ; go vet ./... && go test ./internal/... -count=1`
Expected: no `gofmt` output; `ok` for both packages. Every `plan:` line of `TestPhasesGolden` is unchanged, including "destroy" (`plan: null`), the four unknown cases (the proposed plan comes back untouched), and the C1 diagnostic.

- [ ] **Step 6: Run the plan-heavy acceptance tests**

Run: `make testacc TESTARGS='-run "OutputsKnownAtPlanTime|OutputsUnknownWhenInputsUnknown|FeedsFrom|FeedsAnother|TestAccShared/(LeaverStamped|ExpiresAfter|Unknown|ResetClearsStamps|ReplacementClearsStamps)"'`
Expected: every listed test passes; final line `ok`.

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/core.go internal/provider/resource_list.go internal/provider/resource_set.go internal/provider/resource_map.go
git commit -m "refactor: share ModifyPlan through planPhase"
```

---

### Task B5: Share Create and Update through applyPhase

**Files:**
- Modify: `internal/provider/core.go`, `internal/provider/resource_list.go`, `internal/provider/resource_set.go`, `internal/provider/resource_map.go`

**Interfaces:**
- Consumes: `applyStamps`, `applyFallback`, `newStampArgs`, `nextFunc`, the per-resource `*Next` functions.
- Produces:
  - `type applyCall[C any, V attr.Value] struct { plan *coreModel; planAccumulated, planOutputs V; state *coreModel; stateAccumulated, stateOutputs V; planInputs C; next nextFunc[C] }`
  - `func applyPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], call applyCall[C, V]) (accumulated, outputs V, diags diag.Diagnostics)`
- Removes: the `priorAccumulated` methods on `listResourceModel`, `setResourceModel`, and `mapResourceModel`; the `diag` import of the three resource files.

`applyPhase` reads the clock once, before the known/fallback split; the old Create read it inside each branch. Both read it exactly once per call.

- [ ] **Step 1: Append the apply phase to core.go**

Append after `planPhase`:

```go
// applyCall is one Create or Update call's per-resource input to
// applyPhase.
type applyCall[C any, V attr.Value] struct {
	// plan is the planned shared attributes; applyPhase writes its
	// detailed_outputs and id.
	plan *coreModel
	// planAccumulated and planOutputs are the planned collections.
	planAccumulated, planOutputs V
	// state is the prior shared attributes, nil for Create.
	state *coreModel
	// stateAccumulated and stateOutputs are the prior collections; ignored
	// for Create.
	stateAccumulated, stateOutputs V
	// planInputs is the decoded planned inputs.
	planInputs C
	// next computes the next accumulated value.
	next nextFunc[C]
}

// applyPhase is the shared body of Create and Update. Create runs it with a
// nil state: that is the reseed case, with no prior store and no prior
// stamps, so every value enters (2026-10-15-accumulator-delayed-for-design.md
// T4). It also runs after a replacement, which is what makes
// triggers_replacement reseed. triggers_replacement is never inspected for
// Update: a change to it plans a replacement, and Update is not called.
//
// Known path: ModifyPlan already computed accumulated, outputs,
// detailed_outputs, and the id without reading the clock
// (2026-10-15-accumulator-delayed-for-design.md section 6). Membership is
// realized verbatim (the returned collections are the planned ones), and
// the stamp classification is reproduced through the same pure functions,
// so only the fresh entries (unknown in the plan) are resolved, with the
// apply clock. That is what keeps the applied value identical to the
// planned one.
//
// Fallback path: the plan is unknown (an unknown branch input at plan
// time), the plan promised unknown, and the apply decides membership and
// both stamps with its own clock through applyFallback.
func applyPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], call applyCall[C, V]) (accumulated, outputs V, diags diag.Diagnostics) {
	var prior C
	var priorStamps stamps
	reseed := true
	if call.state != nil {
		var d diag.Diagnostics
		prior, d = priorAccumulated(ctx, s, call.stateAccumulated, call.stateOutputs)
		diags.Append(d...)
		priorStamps, d = stampsFromMap(ctx, call.state.DetailedOutputs)
		diags.Append(d...)
		if diags.HasError() {
			return accumulated, outputs, diags
		}
		reseed = !call.plan.TriggersReset.Equal(call.state.TriggersReset)
	}
	var priorKeys []string
	if !reseed {
		priorKeys = s.keys(prior)
	}
	args := newStampArgs(s.keys(call.planInputs), call.plan, call.state, priorKeys, priorStamps, reseed)
	now := clockNow()

	if !call.planOutputs.IsUnknown() && !call.planAccumulated.IsUnknown() && !call.plan.DetailedOutputs.IsUnknown() {
		planned, d := s.decode(ctx, call.planAccumulated)
		diags.Append(d...)
		detailed, d := applyStamps(ctx, s.keys(planned), args, now)
		diags.Append(d...)
		if diags.HasError() {
			return accumulated, outputs, diags
		}
		call.plan.DetailedOutputs = detailed
		if call.state != nil {
			call.plan.ID = call.state.ID
		}
		return call.planAccumulated, call.planOutputs, diags
	}

	merged := call.next(reseed, prior)
	survivors, visible, detailed, d := applyFallback(ctx, s.keys(merged), args, now)
	diags.Append(d...)
	accumulated, d = s.encode(ctx, s.project(merged, survivors))
	diags.Append(d...)
	outputs, d = s.encode(ctx, s.project(merged, visible))
	diags.Append(d...)
	if diags.HasError() {
		return accumulated, outputs, diags
	}
	call.plan.DetailedOutputs = detailed
	if call.state != nil {
		call.plan.ID = call.state.ID
	} else {
		call.plan.ID = types.StringValue(s.hash(call.planInputs))
	}
	return accumulated, outputs, diags
}
```

- [ ] **Step 2: Route Create through applyPhase**

Replace `listResource.Create` with:

```go
// Create seeds accumulated from the planned inputs through applyPhase. It
// also runs after a replacement, which is what makes triggers_replacement
// reseed.
func (r *listResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	inputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	acc, out, d := applyPhase(ctx, listShape, applyCall[[]string, types.List]{
		plan:            &plan.coreModel,
		planAccumulated: plan.Accumulated,
		planOutputs:     plan.Outputs,
		planInputs:      inputs,
		next:            listNext(inputs, false, int(plan.Length.ValueInt64())),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

Replace `setResource.Create` with:

```go
// Create seeds accumulated from the planned inputs through applyPhase. It
// also runs after a replacement, which is what makes triggers_replacement
// reseed.
func (r *setResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	inputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	acc, out, d := applyPhase(ctx, setShape, applyCall[[]string, types.Set]{
		plan:            &plan.coreModel,
		planAccumulated: plan.Accumulated,
		planOutputs:     plan.Outputs,
		planInputs:      inputs,
		next:            setNext(inputs),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

Replace `mapResource.Create` with:

```go
// Create seeds accumulated from the planned inputs through applyPhase. It
// also runs after a replacement, which is what makes triggers_replacement
// reseed.
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

	acc, out, d := applyPhase(ctx, mapShape, applyCall[map[string]string, types.Map]{
		plan:            &plan.coreModel,
		planAccumulated: plan.Accumulated,
		planOutputs:     plan.Outputs,
		planInputs:      inputs,
		next:            mapNext(inputs),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

- [ ] **Step 3: Route Update through applyPhase**

Replace `listResource.Update` with:

```go
// Update realizes the plan through applyPhase.
func (r *listResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	inputsChanged := !plan.Inputs.Equal(state.Inputs)

	acc, out, d := applyPhase(ctx, listShape, applyCall[[]string, types.List]{
		plan:             &plan.coreModel,
		planAccumulated:  plan.Accumulated,
		planOutputs:      plan.Outputs,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             listNext(planInputs, inputsChanged, int(plan.Length.ValueInt64())),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

Replace `setResource.Update` with:

```go
// Update realizes the plan through applyPhase.
func (r *setResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)

	acc, out, d := applyPhase(ctx, setShape, applyCall[[]string, types.Set]{
		plan:             &plan.coreModel,
		planAccumulated:  plan.Accumulated,
		planOutputs:      plan.Outputs,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             setNext(planInputs),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

Replace `mapResource.Update` with:

```go
// Update realizes the plan through applyPhase.
func (r *mapResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planInputs, d := stringMapFromMap(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)

	acc, out, d := applyPhase(ctx, mapShape, applyCall[map[string]string, types.Map]{
		plan:             &plan.coreModel,
		planAccumulated:  plan.Accumulated,
		planOutputs:      plan.Outputs,
		state:            &state.coreModel,
		stateAccumulated: state.Accumulated,
		stateOutputs:     state.Outputs,
		planInputs:       planInputs,
		next:             mapNext(planInputs),
	})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Accumulated, plan.Outputs = acc, out
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

- [ ] **Step 4: Remove the per-model priorAccumulated methods and the diag import**

Delete `listResourceModel.priorAccumulated`, `setResourceModel.priorAccumulated`, and `mapResourceModel.priorAccumulated` with their doc comments; the package-level `priorAccumulated` in `core.go` replaced them. Nothing in the three resource files uses `diag` any more, so each file's import block becomes:

```go
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)
```

- [ ] **Step 5: Run the unit suite**

Run: `gofmt -l . ; go vet ./... && go test ./internal/... -count=1`
Expected: no `gofmt` output; `ok` for both packages. Every `apply known:` and `apply fallback:` line of `TestPhasesGolden` is unchanged, including the replacement case (Create with a fresh id) and the "expires_after increase" case, whose fallback keeps `"2999-01-01T00:00:00Z"` (max of old and new).

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/core.go internal/provider/resource_list.go internal/provider/resource_set.go internal/provider/resource_map.go
git commit -m "refactor: share Create and Update through applyPhase"
```

---

### Task B6: Share ImportState through importPhase

**Files:**
- Modify: `internal/provider/core.go`, `internal/provider/resource_list.go`, `internal/provider/resource_set.go`, `internal/provider/resource_map.go`

**Interfaces:**
- Consumes: `shape.parse`, `shape.encode`, `shape.keys`, `mapFromStamps`, `importedCore`.
- Produces: `func importPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], id string) (inputs, seeded C, collection V, detailed types.Map, diags diag.Diagnostics)`

What stays per resource: which seed the id hashes (the list hashes its seeded `inputs`, the set and map their seeded `outputs`) and how `inputs` is encoded (the set's `inputs` is a list while its collections are sets, so `shape.encode` cannot write it).

- [ ] **Step 1: Append the import phase to core.go**

Append after `applyPhase`:

```go
// importPhase parses an import ID with s.parse and builds the imported
// collection: accumulated and outputs both hold the seeded outputs, and
// detailed_outputs has an all-null entry per key (2026-09-23 expiry spec
// section 8). It returns the seed so the resource can encode its inputs and
// choose what the id hashes. A parse error becomes the "Invalid import ID"
// diagnostic, whose detail names the expected shape and quotes the
// offending key only.
func importPhase[C any, V attr.Value](ctx context.Context, s shape[C, V], id string) (inputs, seeded C, collection V, detailed types.Map, diags diag.Diagnostics) {
	inputs, seeded, err := s.parse(id)
	if err != nil {
		diags.AddError("Invalid import ID", err.Error())
		return inputs, seeded, collection, detailed, diags
	}
	collection, d := s.encode(ctx, seeded)
	diags.Append(d...)
	detailed, d = mapFromStamps(ctx, s.keys(seeded), stamps{})
	diags.Append(d...)
	return inputs, seeded, collection, detailed, diags
}
```

- [ ] **Step 2: Route ImportState through importPhase**

Replace `listResource.ImportState` with:

```go
// ImportState seeds the resource from a JSON object through importPhase.
// length, triggers_reset, and triggers_replacement are left null: length is
// required configuration, so the next plan fills it in and Update re-trims
// the seeded outputs. Seeding inputs is what lets a matching configuration
// produce an empty plan instead of appending the configured list a second
// time, and id hashes the seeded inputs.
func (r *listResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seedInputs, _, outputs, detailed, d := importPhase(ctx, listShape, req.ID)
	resp.Diagnostics.Append(d...)
	inputs, d := listFromStrings(ctx, seedInputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &listResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashID(seedInputs)),
		Inputs:      inputs,
		Length:      types.Int64Null(),
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
```

Replace `setResource.ImportState` with:

```go
// ImportState seeds the set from a JSON object through importPhase. outputs
// is deduplicated and id hashes the deduplicated outputs, not the raw seed:
// a seed such as {"outputs":["a","a","b"]} then yields the same id as
// {"outputs":["a","b"]}, matching the state it produces
// (2026-09-17-terraform-provider-accumulator-design.md section 8). inputs is
// seeded as an empty list rather than null: it is a required attribute, and
// an empty list is a valid non-null value, so nothing depends on tolerating
// a null required attribute in imported state. The next plan sets inputs
// from configuration and Update unions it into the seeded outputs.
func (r *setResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	_, seeded, outputs, detailed, d := importPhase(ctx, setShape, req.ID)
	resp.Diagnostics.Append(d...)
	inputs, d := listFromStrings(ctx, []string{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &setResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashID(seeded)),
		Inputs:      inputs,
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
```

Replace `mapResource.ImportState` with:

```go
// ImportState seeds the map from a JSON object through importPhase. id
// hashes the seeded outputs. inputs is seeded as an empty map rather than
// null: it is a required attribute, and an empty map is a valid non-null
// value. The next plan sets inputs from configuration and Update merges it
// into the seeded outputs.
func (r *mapResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	_, seeded, outputs, detailed, d := importPhase(ctx, mapShape, req.ID)
	resp.Diagnostics.Append(d...)
	inputs, d := mapFromStringMap(ctx, map[string]string{})
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &mapResourceModel{
		coreModel:   importedCore(detailed, accumulate.HashMapID(seeded)),
		Inputs:      inputs,
		Accumulated: outputs,
		Outputs:     outputs,
	})...)
}
```

- [ ] **Step 3: Run the unit suite**

Run: `gofmt -l . ; go vet ./... && go test ./internal/... -count=1`
Expected: no `gofmt` output; `ok` for both packages. Every `import` section of `TestPhasesGolden` is unchanged, including the five `Invalid import ID` diagnostics per resource shape.

- [ ] **Step 4: Run the import acceptance tests**

Run: `make testacc TESTARGS='-run Import' && make testacc TESTARGS='-run TestAccShared/Import'`
Expected: both runs end with `ok`. The first runs the 8 tests in `import_test.go` with all 10 `TestAccImportMalformed` subtests; the second runs `ImportThenExpiresAfter` and `ImportThenDelayedFor` for all three flavors. (They are separate runs because `-run` applies the part after `/` to every matched test's subtests, which would filter out the malformed-import cases.)

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/core.go internal/provider/resource_list.go internal/provider/resource_set.go internal/provider/resource_map.go
git commit -m "refactor: share ImportState through importPhase"
```

---

### Task B7: Compare the refactored provider against the baseline

**Files:** none changed.

- [ ] **Step 1: Rerun the full acceptance suite and compare**

```bash
make testacc TESTARGS='-count=1' > /tmp/accumulator-core/acc-after.log 2>&1; echo "exit $?"
grep -E '^\s*--- (PASS|FAIL|SKIP): TestAcc' /tmp/accumulator-core/acc-after.log | sed 's/ (.*//' | sort > /tmp/accumulator-core/acc-after.txt
diff /tmp/accumulator-core/acc-before.txt /tmp/accumulator-core/acc-after.txt && echo SAME
grep -c 'FAIL' /tmp/accumulator-core/acc-after.txt
```

Expected: `exit 0`; `SAME`; `0`.

- [ ] **Step 2: Compare the exported provider schema byte for byte**

```bash
./tools/gen-schema.sh && cmp tools/schema.json /tmp/accumulator-core/schema-before.json && echo SCHEMA-IDENTICAL
```

Expected: `SCHEMA-IDENTICAL`. This covers every attribute name, type, flag, plan modifier marker, and `MarkdownDescription`.

- [ ] **Step 3: Confirm the generated docs do not move**

Run: `make docs && git status --porcelain docs/`
Expected: no output (the CI docs job runs the same check).

- [ ] **Step 4: Confirm the golden file was never regenerated**

Run: `git log --oneline "$(cat /tmp/accumulator-core/before.rev)"..HEAD -- internal/provider/testdata/phases.golden`
Expected: exactly one line, the Task B1 commit `test: pin phase outputs with a golden file`.

- [ ] **Step 5: Record the line counts**

Run: `git diff --stat "$(cat /tmp/accumulator-core/before.rev)"..HEAD -- internal/provider/ ':!internal/provider/*_test.go' ':!internal/provider/testdata'`
Expected (reference tree): `core.go` +404; `resource_list.go` 550 → 338, `resource_set.go` 549 → 335, `resource_map.go` 533 → 336; production code 1,632 → 1,413 lines (−219). Tests add `golden_test.go` (652), `core_test.go` (78), and `testdata/phases.golden` (341).

No commit: the tree is unchanged.

---

## Self-review

**Spec coverage.**
- Review recommendation 4 (shared matrix): Tasks A4 and A5 (28 rows × 3 flavors); map import tests beside the others: Task A3; list Read-fills-null: Task A2; `setDetail` → `oneDetail`: Task A1. The inventory accounts for all 122 existing acceptance tests: 50 kept (list 22, set 10, map 13, import 5), 3 moved, 1 folded, and 68 merged into rows (list 20, set 28, map 20, the map's `EnteringKeyHidden` among them). 50 + 3 = 53 top-level tests outside the matrix, plus `TestAccShared`, is the 54.
- Review section 7 asymmetries: every map row in E1 to E16 now runs, including `UnknownExpiresAfterAtPlan`, `ReplacementClearsStamps`, and the upper bound; the list gains the delay decrease, increase, reset-delays-seed, and import-then-delay rows. Task A4 Step 8 shows the matrix fails on the review's PriorExpires mistake.
- Review recommendation 5 (phase core): embedded `coreModel` (Task B2), `shape` (Task B3), `planPhase` (Task B4), `applyPhase` for the Create/Update known and fallback paths (Task B5), `readPhase` (Task B3), plus `importPhase` (Task B6). Each resource keeps its `Schema`, `Metadata`, `ValidateConfig`, `Delete`, descriptions, model, shape value, and accumulation step.
- Delay spec section 6 (ModifyPlan reads no clock; known path realizes membership verbatim; fallback uses the apply clock): `planPhase` calls no clock; `applyPhase` returns the planned collections on the known path; pinned by `TestPhasesGolden`.
- Delay spec section 9 and map spec section 10 (null `accumulated` reads as `outputs`): `priorAccumulated` in `core.go`; pinned by the three Read unit tests and the golden's "pre-feature state" and "accumulated null" cases.
- Expiry spec section 8 (import seeds null stamps): `importPhase`; pinned by `importSeedsNullDetails` (E15) and the golden import sections.
- State compatibility: no tag or type changes (Task B2 Step 5; Task B7 Step 2).
- Determinism: `TestPhasesGolden` after every Part B task; the acceptance before/after comparison (Task B1 Step 1, Task B7 Step 1).

**Placeholder scan.** Every code step carries the complete declaration or file it writes. Every deletion step names each declaration. Commands have expected output. The one generated artifact, `testdata/phases.golden`, is produced by a command in Task B1 Step 3 and spot-checked there.

**Type consistency.**
- `flavor.values` takes `[]string`; the four `flavor` methods take `...string` and pass the slice through. `flavor.importID(inputs, outputs []string)`: the list uses `inputs` (nil omits the key), the set and map ignore it. `ids(...)` returns `[]string` for `config` and `importID`.
- `sharedCase.steps` returns `[]resource.TestStep`; `sharedGroups` is `[][]sharedCase`, `{expiryCases}` after Task A4 and `{expiryCases, delayCases}` after Task A5.
- `shape[C, V]`: `listShape` is `shape[[]string, types.List]`, `setShape` is `shape[[]string, types.Set]`, `mapShape` is `shape[map[string]string, types.Map]`. `sliceShape` fills `keys`, `project`, and `hash` for both slice shapes. Every `planCall`/`applyCall` literal names the same `[C, V]` pair as the shape passed beside it.
- `nextFunc[C]` is `func(reseed bool, prior C) C`; `listNext`, `setNext`, `mapNext` bind `planInputs` (and the list's `inputsChanged` and `length`). Create passes `inputsChanged = false` to `listNext`, matching the old `NextListOutputs(true, false, …)`.
- `planPhase` returns `(accumulated, outputs V, ok bool, diags)`; `applyPhase` returns `(accumulated, outputs V, diags)`; `readPhase` returns `(accumulated, outputs V, changed bool, diags)`; `importPhase` returns `(inputs, seeded C, collection V, detailed types.Map, diags)`. All four write `DetailedOutputs` (and `planPhase`/`applyPhase` the `ID`) through the `*coreModel` they are given; every resource assigns the two returned collections to its own fields.
- `stampArgs.PlanInputs` and `PriorAccumulated` are `[]string`, always built with `s.keys`, which is the identity for the list and set and `accumulate.SortedKeys` for the map, as in the old code.
