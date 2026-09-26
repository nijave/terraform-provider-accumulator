# Accumulator Expiration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `expires_after` (optional TTL in seconds) and `detailed_outputs` (computed map of value to `{expires_at}`) to both accumulator resources, with all time-dependent decisions made by the plan-time clock.

**Architecture:** A new pure function `accumulate.ApplyExpiration` filters the accumulated result for expiry with the clock injected as a parameter; ModifyPlan calls it with the plan clock and writes fully-known `outputs` and `detailed_outputs` into the plan; Create/Update copy those known planned values verbatim, and only compute with the apply clock on the fallback path (plan unknown). New provider-boundary helpers convert between `types.Map` state and `map[string]time.Time`. Import is unchanged except for seeding all-null `detailed_outputs`.

**Tech Stack:** unchanged: Go, `terraform-plugin-framework` v1.19.0, `terraform-plugin-framework-validators`, `terraform-plugin-testing` v1.16.0 (its `knownvalue` package supplies `MapExact`, `ObjectExact`, `NullExact`, `StringRegexp`), tfplugindocs via the `tools/` module, OpenTofu for acceptance tests.

**Spec:** `docs/superpowers/specs/2026-09-23-accumulator-expiration-design.md` (approved). Read it alongside this plan; the plan argues from it.

## Global Constraints

Every task's requirements implicitly include this section.

- **The clock is the plan-time clock.** ModifyPlan computes every stamp, min/max recalculation, and removal with `time.Now()` at plan time; Update/Create copy known planned values verbatim; the apply clock is used only when the plan is unknown (spec section 6). All clock reads go through `clockNow()` (Task 2): `time.Now().UTC().Truncate(time.Second)`, so stamps round-trip RFC 3339 state strings exactly.
- **`internal/accumulate` stays Terraform-free.** `expire.go` imports only `time`. The existing `boundary_test.go` fails the build if any `terraform-plugin-*` import appears there.
- **In `inputs` means null.** A value in the planned `inputs` never gets a stamp and is never removed (spec R2). `detailed_outputs` carries `expires_at = null` for it.
- **Removal boundary is inclusive:** `expires_at <= now` removes (spec X1), evaluated after stamping, against final stamps.
- **`expires_after` is optional int64 seconds, `int64validator.AtLeast(0)`.** No plan modifier; changes are in-place updates.
- **`detailed_outputs` is computed, one entry per distinct value in `outputs`, always concrete** (`{}` when empty, never null). Its element type is an object with one nullable string attribute `expires_at`.
- **Import contract unchanged.** No new import-ID keys. Imported state seeds `ExpiresAfter` null and `detailed_outputs` with all-null entries (spec section 8).
- **License GPL-3.0-or-later.** Every new `.go` file starts with `// SPDX-License-Identifier: GPL-3.0-or-later` (existing `TestEveryGoFileHasTheSPDXHeader` walks the module).
- **Em dashes in user-facing strings.** `TestUserFacingStringsUseEmDashes` fails any `--` inside a non-test string literal (schema descriptions included). Comments may use `--`.
- **House checks per task:** `gofmt -l .` empty, `go vet ./...` clean, `go test ./internal/...` green. Acceptance tests run with `make testacc TESTARGS='-run <Pattern>'` (the makefile sets `TF_ACC`, `TF_ACC_TERRAFORM_PATH=$(command -v tofu)`, `TF_ACC_PROVIDER_HOST=registry.opentofu.org`; `tofu` v1.12.1 is on PATH here).
- **Docs are generated, never hand-edited** (`make docs`, requires tofu >= 1.11). `docs/superpowers/` is hand-written and must not be clobbered. CI fails on a dirty tree after generation, so Task 5 must run `make docs` and commit the result.
- **Conventional Commits; stage explicit paths;** never `git add -A`.

## Review Focus

The five failure modes the design depends on that a reasonable user would expect to work, most likely to bite first. Each is pinned by the test named here in its owning task.

1. **A bare apply with an expired value must realize the removal** — OpenTofu has to treat a computed-attribute plan diff (known values differing from state) as an update and call Update; if it did not, expired values would linger forever. Pinned by `TestAccSetExpiryRealizedOnBareApply` step 3 (Task 3) and `TestAccListExpiryRealizedOnBareApply` step 3 (Task 4).
2. **Steady-state plan churn** — with `expires_after` set but nothing expired and no config change, the plan must be empty; perpetual diffs would break every pipeline using the resource. Pinned by `TestAccSetInInputsNeverExpires` step 2 (Task 3) and by every `PostApplyPostRefresh: ExpectEmptyPlan` check on a 3600s window.
3. **Plan/apply divergence at the expiry boundary** — a value whose window ends between plan and apply must not produce an inconsistent-result error; the verbatim-copy known path is the mechanism. Pinned by `TestAccSetLeaverStampedOnInputsChange`'s post-refresh empty plan (Task 3); the fallback path by `TestAccSetUnknownExpiresAfterAtPlan` (Task 3).
4. **Pre-expiration state files** — state written before this feature lacks both attributes; Get must unmarshal it with them null rather than erroring, or every upgrade breaks. Pinned by `TestStateGetToleratesMissingExpirationAttributes` (Task 2).
5. **Sub-second round-trip skew** — a nanosecond stamp formats to a second-truncated string and would compare up to a second early on the next plan, expiring values ahead of schedule; `clockNow()` truncation is the mechanism. Pinned by the exact-stamp `wantStamp` comparisons in `TestApplyExpiration` (Task 1) and the `TestMapFromStampsAndBack` round trip (Task 2).

Also unit-pinned rather than acceptance-pinned: the min/max directionality on `expires_after` changes (Task 1 table) and the inclusive removal boundary `expires_at <= now` (Task 1 table).

## File Structure

| File | Responsibility | Task |
| --- | --- | --- |
| `internal/accumulate/expire.go` | `ApplyExpiration`: pure expiry filter + stamping | 1 |
| `internal/accumulate/expire_test.go` | Table-driven unit tests for it | 1 |
| `internal/provider/model.go` | `detailedObjectType`, `detailAttr`, `clockNow`, `int64Ptr`, `stampsFromMap`, `mapFromStamps` | 2 |
| `internal/provider/model_test.go` | Unit tests for the helpers; state-shape tolerance test | 2 |
| `internal/provider/resource_set.go` | `accumulator_set` wiring: schema, model, Create/Update/ModifyPlan/ImportState | 3 |
| `internal/provider/provider_test.go` | Shared acceptance helpers: `nullDetail`, `stampedDetail`, `expectDetailed`, `expectPlanDetailed`, `sleepPastExpiry` | 3 |
| `internal/provider/resource_set_test.go` | Set acceptance tests | 3 |
| `internal/provider/resource_list.go` | `accumulator_list` wiring | 4 |
| `internal/provider/resource_list_test.go` | List acceptance tests | 4 |
| `internal/provider/import_test.go` | One detailed-outputs assertion added to `TestAccListImport`'s settling step | 4 |
| `examples/resources/accumulator_set/resource.tf`, `examples/resources/accumulator_list/resource.tf` | Show `expires_after` and `detailed_outputs` | 5 |
| `docs/index.md`, `docs/resources/*.md` | Regenerated from schema + examples | 5 |

---

### Task 1: `accumulate.ApplyExpiration`

The pure decision both resources share. Deliberately identical in shape to `NextListOutputs`/`NextSetOutputs`: one function ModifyPlan and Update both call, so plan and apply cannot disagree on the known path.

**Files:**
- Create: `internal/accumulate/expire.go`
- Test: `internal/accumulate/expire_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `func ApplyExpiration(result, planInputs []string, prior map[string]time.Time, priorExpiresAfter, plannedExpiresAfter *int64, now time.Time) ([]string, map[string]time.Time)` — survivors in `result` order, and stamps for survivors not in `planInputs` (absence = null `expires_at`). Both return values non-nil.

- [ ] **Step 1: Write the failing tests**

Create `internal/accumulate/expire_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"testing"
	"time"
)

// seconds is the test shorthand for the *int64 expires_after parameters.
func seconds(n int64) *int64 { return &n }

// stampBase is the fixed clock every case computes against. now must be
// truncated to whole seconds, matching clockNow in internal/provider, so
// stamps formatted RFC 3339 round-trip exactly.
var stampBase = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

// TestApplyExpiration pins the whole stamping and removal decision. Every
// rule letter cites the spec (2026-09-23-accumulator-expiration-design.md,
// section 4).
func TestApplyExpiration(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		result    []string
		inputs    []string
		prior     map[string]time.Time
		priorE    *int64
		plannedE  *int64
		want      []string
		wantStamp map[string]time.Time
	}{
		"leaver with no prior stamp is stamped now+E (S1)": {
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    nil,
			plannedE: seconds(300),
			want:     []string{"a", "b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(5 * time.Minute),
			},
		},
		"in-inputs value is never stamped and never removed (R2)": {
			result:   []string{"a"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"a": stampBase.Add(-1 * time.Hour)},
			plannedE: seconds(1),
			want:     []string{"a"},
			wantStamp: map[string]time.Time{},
		},
		"removal boundary is inclusive (X1)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase},
			priorE:   seconds(300),
			plannedE: seconds(300),
			want:     []string{},
		},
		"future stamp is kept unchanged (S2, X1 negative)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(1 * time.Second)},
			priorE:   seconds(300),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(1 * time.Second),
			},
		},
		"no prior stamp with E just set gets the candidate (S5)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    nil,
			priorE:   nil,
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(5 * time.Minute),
			},
		},
		"stamped value with E set later keeps no clock until E exists (S5 via null priorE)": {
			// History that accumulated while E was null: prior stamp absent,
			// priorE nil, so the first E-setting plan stamps the candidate.
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{},
			priorE:   nil,
			plannedE: seconds(60),
			want:     []string{"a", "b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(1 * time.Minute),
			},
		},
		"decrease keeps min(old, candidate) when candidate is earlier (S3)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(10 * time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(5 * time.Minute),
			},
		},
		"decrease keeps min(old, candidate) when old is earlier (S3)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(1 * time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(1 * time.Minute),
			},
		},
		"increase keeps max(old, candidate) when candidate is later (S4)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(1 * time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(600),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(10 * time.Minute),
			},
		},
		"increase keeps max(old, candidate) when old is later (S4)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(10 * time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(10 * time.Minute),
			},
		},
		"increase rescues an expired-unrealized value (S4 plus X1)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(600),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(10 * time.Minute),
			},
		},
		"decrease never rescues an expired-unrealized value (S3 plus X1)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     []string{},
		},
		"E removed expires nothing and stamps nothing (T2)": {
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Minute)},
			priorE:   seconds(60),
			plannedE: nil,
			want:     []string{"a", "b"},
		},
		"E of zero removes a leaver at the same call (X1)": {
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    nil,
			plannedE: seconds(0),
			want:     []string{"a"},
		},
		"duplicate values expire together and order is preserved (list shape)": {
			result:   []string{"a", "b", "b", "c"},
			inputs:   []string{"a", "c"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Second)},
			priorE:   seconds(300),
			plannedE: seconds(300),
			want:     []string{"a", "c"},
		},
		"create shape: everything in inputs yields no stamps (T4)": {
			result:    []string{"a", "b"},
			inputs:    []string{"a", "b"},
			prior:     map[string]time.Time{"a": stampBase.Add(-1 * time.Hour)},
			plannedE:  seconds(300),
			want:      []string{"a", "b"},
			wantStamp: map[string]time.Time{},
		},
		"empty result yields empty survivors and empty stamps": {
			result:    []string{},
			inputs:    nil,
			plannedE:  seconds(300),
			want:      []string{},
			wantStamp: map[string]time.Time{},
		},
		"nil result with E set yields empty survivors": {
			result:   nil,
			inputs:   nil,
			plannedE: seconds(300),
			want:     []string{},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, gotStamps := ApplyExpiration(tc.result, tc.inputs, tc.prior, tc.priorE, tc.plannedE, stampBase)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("survivors = %v, want %v", got, tc.want)
			}
			if got == nil {
				t.Fatal("survivors is nil; it must always be non-nil")
			}
			wantStamps := tc.wantStamp
			if wantStamps == nil {
				wantStamps = map[string]time.Time{}
			}
			if !reflect.DeepEqual(gotStamps, wantStamps) {
				t.Fatalf("stamps = %v, want %v", gotStamps, wantStamps)
			}
			if gotStamps == nil {
				t.Fatal("stamps is nil; it must always be non-nil")
			}
		})
	}
}

// TestApplyExpirationIsDeterministic pins that the clock is the only input
// that varies: identical arguments produce identical results (spec 10.1).
func TestApplyExpirationIsDeterministic(t *testing.T) {
	t.Parallel()
	args := []any{
		[]string{"a", "b"}, []string{"a"},
		map[string]time.Time{"b": stampBase.Add(time.Minute)},
		seconds(60), seconds(300), stampBase,
	}
	first, firstStamps := ApplyExpiration(
		args[0].([]string), args[1].([]string), args[2].(map[string]time.Time),
		args[3].(*int64), args[4].(*int64), args[5].(time.Time))
	second, secondStamps := ApplyExpiration(
		args[0].([]string), args[1].([]string), args[2].(map[string]time.Time),
		args[3].(*int64), args[4].(*int64), args[5].(time.Time))
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstStamps, secondStamps) {
		t.Fatalf("ApplyExpiration is not deterministic: %v/%v vs %v/%v", first, firstStamps, second, secondStamps)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/accumulate/ -run TestApplyExpiration -v`
Expected: FAIL, `undefined: ApplyExpiration`.

- [ ] **Step 3: Implement `ApplyExpiration`**

Create `internal/accumulate/expire.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import "time"

// ApplyExpiration filters an accumulated result for expiry and returns the
// survivors with their stamps. It is the expiration half of the next-state
// decision (2026-09-23-accumulator-expiration-design.md, sections 4 and 7)
// and runs after the accumulation and trim, never before, so expiration can
// never resurrect a trimmed value.
//
// Per value of result:
//
//   - plannedExpiresAfter nil: nothing expires and nothing is stamped.
//   - a value in planInputs survives with no stamp; it cannot expire.
//   - otherwise the stamp is candidate = now + plannedExpiresAfter when
//     there is no prior stamp or no prior expiresAfter; min(prior,
//     candidate) on a decrease; max(prior, candidate) on an increase; the
//     prior stamp unchanged when expiresAfter did not change.
//   - after stamping, a value whose stamp is not strictly after now is
//     removed. An increase can therefore rescue an expired-but-unrealized
//     value, and expiresAfter 0 removes a value in the same call that
//     stamps it.
//
// now must already be truncated to whole seconds (clockNow in
// internal/provider is the choke point) so stamps round-trip RFC 3339 state
// strings without losing a fraction of a second.
//
// Both return values are non-nil. Stamps are only present for survivors not
// in planInputs; absence means expires_at is null.
func ApplyExpiration(result, planInputs []string,
	prior map[string]time.Time,
	priorExpiresAfter, plannedExpiresAfter *int64,
	now time.Time) ([]string, map[string]time.Time) {

	survivors := make([]string, 0, len(result))
	stamps := make(map[string]time.Time, len(result))
	if plannedExpiresAfter == nil {
		return append(survivors, result...), stamps
	}

	supplied := make(map[string]struct{}, len(planInputs))
	for _, v := range planInputs {
		supplied[v] = struct{}{}
	}

	for _, v := range result {
		if _, ok := supplied[v]; ok {
			survivors = append(survivors, v)
			continue
		}

		candidate := now.Add(time.Duration(*plannedExpiresAfter) * time.Second)
		stamp, had := prior[v]
		switch {
		case !had || priorExpiresAfter == nil:
			stamp = candidate
		case *priorExpiresAfter > *plannedExpiresAfter:
			if candidate.Before(stamp) {
				stamp = candidate
			}
		case *priorExpiresAfter < *plannedExpiresAfter:
			if candidate.After(stamp) {
				stamp = candidate
			}
		}

		if !stamp.After(now) {
			continue
		}
		survivors = append(survivors, v)
		stamps[v] = stamp
	}
	return survivors, stamps
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/accumulate/ -v`
Expected: PASS, including the pre-existing tests and `TestPackageImportsNoTerraform` (only the stdlib `time` import was added).

- [ ] **Step 5: House checks and commit**

```bash
gofmt -l . && go vet ./... && go test ./internal/...
git add internal/accumulate/expire.go internal/accumulate/expire_test.go
git commit -m "feat: add ApplyExpiration to the accumulate package"
```

---

### Task 2: Provider boundary helpers and state-shape tolerance

The translation layer between `types.Map` state and the pure function's `map[string]time.Time`, plus the clock choke point, plus the proof that pre-expiration state (missing both new attributes) unmarshals cleanly (spec section 9).

**Files:**
- Modify: `internal/provider/model.go`
- Test: `internal/provider/model_test.go`

**Interfaces:**
- Consumes: `accumulate.ApplyExpiration` (not yet called; Task 3 and 4 do).
- Produces (all in package `provider`):
  - `var detailedObjectType types.ObjectType` — element type of `detailed_outputs`
  - `func clockNow() time.Time`
  - `func int64Ptr(v types.Int64) *int64`
  - `func stampsFromMap(ctx context.Context, m types.Map) (map[string]time.Time, diag.Diagnostics)`
  - `func mapFromStamps(ctx context.Context, outputs []string, stamps map[string]time.Time) (types.Map, diag.Diagnostics)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/provider/model_test.go` (it is `package provider`, so it can use the unexported helpers directly):

```go
func TestMapFromStampsAndBack(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	outputs := []string{"a", "b", "c"}
	stamps := map[string]time.Time{
		"b": base.Add(5 * time.Minute),
		"c": base.Add(time.Hour),
	}
	m, diags := mapFromStamps(ctx, outputs, stamps)
	if diags.HasError() {
		t.Fatalf("mapFromStamps: %+v", diags)
	}
	if m.IsNull() || m.IsUnknown() {
		t.Fatalf("mapFromStamps returned %v; it must be concrete", m)
	}
	if got := len(m.Elements()); got != 3 {
		t.Fatalf("map has %d entries, want 3", got)
	}

	parsed, diags := stampsFromMap(ctx, m)
	if diags.HasError() {
		t.Fatalf("stampsFromMap: %+v", diags)
	}
	want := map[string]time.Time{
		"b": base.Add(5 * time.Minute),
		"c": base.Add(time.Hour),
	}
	if !reflect.DeepEqual(parsed, want) {
		t.Fatalf("round trip = %v, want %v", parsed, want)
	}

	// The null entry ("a") must not appear: absence is the null stamp.
	if _, ok := parsed["a"]; ok {
		t.Fatal("null expires_at round-tripped as a stamp")
	}
}

func TestMapFromStampsIsNeverNull(t *testing.T) {
	m, diags := mapFromStamps(context.Background(), nil, nil)
	if diags.HasError() {
		t.Fatalf("mapFromStamps: %+v", diags)
	}
	if m.IsNull() {
		t.Fatal("mapFromStamps(nil) returned null; an empty accumulator must be {}")
	}
	if got := len(m.Elements()); got != 0 {
		t.Fatalf("map has %d entries, want 0", got)
	}
}

func TestStampsFromMapNullAndUnknownAreEmpty(t *testing.T) {
	for _, m := range []types.Map{types.MapNull(detailedObjectType), types.MapUnknown(detailedObjectType)} {
		stamps, diags := stampsFromMap(context.Background(), m)
		if diags.HasError() {
			t.Fatalf("stampsFromMap(%v): %+v", m, diags)
		}
		if len(stamps) != 0 {
			t.Fatalf("stampsFromMap(%v) = %v, want empty", m, stamps)
		}
	}
}

func TestStampsFromMapRejectsGarbageTimestamp(t *testing.T) {
	m := types.MapValueMust(detailedObjectType, map[string]attr.Value{
		"b": types.ObjectValueMust(detailedObjectType.AttrTypes, map[string]attr.Value{
			"expires_at": types.StringValue("not-a-timestamp"),
		}),
	})
	_, diags := stampsFromMap(context.Background(), m)
	if !diags.HasError() {
		t.Fatal("stampsFromMap accepted a malformed timestamp; it must diagnose")
	}
}

func TestInt64Ptr(t *testing.T) {
	if got := int64Ptr(types.Int64Null()); got != nil {
		t.Fatalf("int64Ptr(null) = %v, want nil", *got)
	}
	if got := int64Ptr(types.Int64Unknown()); got != nil {
		t.Fatalf("int64Ptr(unknown) = %v, want nil", *got)
	}
	v := types.Int64Value(300)
	if got := int64Ptr(v); got == nil || *got != 300 {
		t.Fatalf("int64Ptr(300) = %v, want 300", got)
	}
}

// TestStateGetToleratesMissingExpirationAttributes pins spec section 9:
// state written by the pre-expiration provider lacks expires_after and
// detailed_outputs entirely, and Get must unmarshal it with both null
// rather than erroring. If this test fails, the correct fix is an
// UpgradeState implementation, not a schema change.
func TestStateGetToleratesMissingExpirationAttributes(t *testing.T) {
	setSchema := resourceSchema(t, &setResource{})
	listSchema := resourceSchema(t, &listResource{})

	stringType := tftypes.String
	setOld := tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"inputs":               tftypes.List{ElementType: stringType},
		"triggers_reset":       stringType,
		"triggers_replacement": stringType,
		"outputs":              tftypes.Set{ElementType: stringType},
		"id":                   stringType,
	}}, map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"outputs":              tftypes.NewValue(tftypes.Set{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"id":                   tftypes.NewValue(stringType, "x"),
	})
	listOld := tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"inputs":               tftypes.List{ElementType: stringType},
		"length":               tftypes.Number,
		"triggers_reset":       stringType,
		"triggers_replacement": stringType,
		"outputs":              tftypes.List{ElementType: stringType},
		"id":                   stringType,
	}}, map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"length":               tftypes.NewValue(tftypes.Number, 5),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"outputs":              tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"id":                   tftypes.NewValue(stringType, "x"),
	})

	for label, tc := range map[string]struct {
		schema schema.Schema
		raw    tftypes.Value
	}{
		"set":  {setSchema, setOld},
		"list": {listSchema, listOld},
	} {
		t.Run(label, func(t *testing.T) {
			st := tfsdk.State{Raw: tc.raw, Schema: tc.schema}
			var probe struct {
				ExpiresAfter    types.Int64 `tfsdk:"expires_after"`
				DetailedOutputs types.Map   `tfsdk:"detailed_outputs"`
			}
			if diags := st.Get(context.Background(), &probe); diags.HasError() {
				t.Fatalf("Get on pre-expiration state shape: %+v", diags)
			}
			if !probe.ExpiresAfter.IsNull() {
				t.Errorf("ExpiresAfter = %v, want null", probe.ExpiresAfter)
			}
			if !probe.DetailedOutputs.IsNull() {
				t.Errorf("DetailedOutputs = %v, want null", probe.DetailedOutputs)
			}
		})
	}
}

// resourceSchema fetches a resource's schema for state-shape tests.
func resourceSchema(t *testing.T, r resource.Resource) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("resource schema: %+v", resp.Diagnostics)
	}
	return resp.Schema
}
```

Add these imports to `model_test.go`: `"reflect"`, `"time"`, `"github.com/hashicorp/terraform-plugin-framework/attr"`, `"github.com/hashicorp/terraform-plugin-framework/resource"`, `"github.com/hashicorp/terraform-plugin-framework/resource/schema"`, `"github.com/hashicorp/terraform-plugin-framework/tfsdk"`, `"github.com/hashicorp/terraform-plugin-go/tftypes"`. `context`, `types`, and `diag` are already imported by the file.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run 'TestMapFromStamps|TestStampsFromMap|TestInt64Ptr|TestStateGetTolerates' -v`
Expected: FAIL to compile: `undefined: mapFromStamps`, `undefined: detailedObjectType`, and so on. Note: `TestStateGetToleratesMissingExpirationAttributes` also needs the `setResource`/`listResource` schemas to still list the old shape; it passes only after Task 3 and 4 add the new attributes, so run it again there. To keep this task green in isolation, gate it:

```go
func TestStateGetToleratesMissingExpirationAttributes(t *testing.T) {
	if _, ok := setSchema.Type(context.Background()).(tftypes.Object).AttributeTypes["expires_after"]; !ok {
		t.Skip("expires_after not in the schema yet; re-run after Tasks 3 and 4")
	}
	// ... rest of the test as written above
}
```

- [ ] **Step 3: Implement the helpers**

Append to `internal/provider/model.go`:

```go
// detailedObjectType is the element type of detailed_outputs on both
// resources: an object whose single attribute is a nullable RFC 3339 string.
var detailedObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{"expires_at": types.StringType},
}

// detailAttr is the Go shape of one detailed_outputs entry. A null
// ExpiresAt means the value cannot expire; it is written as null, never as
// a zero time.
type detailAttr struct {
	ExpiresAt types.String `tfsdk:"expires_at"`
}

// clockNow is the one clock choke point for both the plan-time and
// fallback apply-time reads: UTC truncated to whole seconds, so a stamp
// formatted RFC 3339 parses back to the same instant. A nanosecond stamp
// would lose its fraction on formatting and compare up to a second early on
// the next plan.
func clockNow() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// int64Ptr returns a pointer to the underlying value, or nil for null or
// unknown. Callers on the ModifyPlan path must have resolved unknowns
// already; nil is the "no expires_after" the pure functions expect.
func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	x := v.ValueInt64()
	return &x
}

// stampsFromMap reads detailed_outputs into the value-to-stamp map the pure
// functions take. A null or unknown map (state written before the attribute
// existed) and entries with a null expires_at both mean "no stamp".
func stampsFromMap(ctx context.Context, m types.Map) (map[string]time.Time, diag.Diagnostics) {
	var diags diag.Diagnostics
	stamps := make(map[string]time.Time)
	if m.IsNull() || m.IsUnknown() {
		return stamps, diags
	}
	for key, elem := range m.Elements() {
		obj, ok := elem.(types.Object)
		if !ok {
			diags.AddError("Invalid detailed_outputs state",
				fmt.Sprintf("entry %q is not an object; the state file is corrupt", key))
			return nil, diags
		}
		s, ok := obj.Attributes()["expires_at"].(types.String)
		if !ok {
			diags.AddError("Invalid detailed_outputs state",
				fmt.Sprintf("entry %q has a non-string expires_at; the state file is corrupt", key))
			return nil, diags
		}
		if s.IsNull() || s.IsUnknown() {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, s.ValueString())
		if err != nil {
			diags.AddError("Invalid expires_at in state",
				fmt.Sprintf("entry %q expires_at %q is not an RFC 3339 timestamp: %v", key, s.ValueString(), err))
			return nil, diags
		}
		stamps[key] = stamp
	}
	return stamps, diags
}

// mapFromStamps builds detailed_outputs from the survivors and their
// stamps: one entry per distinct value, expires_at formatted RFC 3339 UTC
// or null when the value has no stamp. Duplicate values in outputs collapse
// into one entry, which is the documented list-resource shape. An empty
// outputs list yields an empty map, never null (the same concreteness rule
// as outputs).
func mapFromStamps(ctx context.Context, outputs []string, stamps map[string]time.Time) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(outputs))
	for _, v := range outputs {
		entry := detailAttr{ExpiresAt: types.StringNull()}
		if stamp, ok := stamps[v]; ok {
			entry.ExpiresAt = types.StringValue(stamp.UTC().Format(time.RFC3339))
		}
		entries[v] = entry
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}
```

Extend `model.go` imports with `"fmt"`, `"time"`, and `"github.com/hashicorp/terraform-plugin-framework/attr"`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/provider/ -run 'TestMapFromStamps|TestStampsFromMap|TestInt64Ptr|TestStateGetTolerates' -v`
Expected: PASS (with the state-shape test skipped until Tasks 3/4).

- [ ] **Step 5: House checks and commit**

```bash
gofmt -l . && go vet ./... && go test ./internal/...
git add internal/provider/model.go internal/provider/model_test.go
git commit -m "feat: add detailed_outputs state boundary helpers"
```

---

### Task 3: `accumulator_set` wiring and acceptance tests

> **SUPERSEDED in part (2026-09-24):** the Step 2 mechanics and several Step 4 tests in this task were reworked for refresh-time culling after the plan-time clock design failed on OpenTofu's double-plan consistency check. Follow the "Revision 2026-09-24 — refresh-culling rework" section at the end of this document; it replaces this task's Step 2 Read/ModifyPlan/Update code and the affected tests. Everything else here stands.

Schema, model, and the plan/apply split for the set resource, plus every set acceptance test the spec's table calls for. `TestProviderSchema` needs no change: it validates every registered resource's schema implementation automatically, so the new attributes are covered the moment they are added.

**Files:**
- Modify: `internal/provider/resource_set.go`
- Modify: `internal/provider/provider_test.go` (shared helpers)
- Test: `internal/provider/resource_set_test.go`

**Interfaces:**
- Consumes: `accumulate.ApplyExpiration`, `clockNow`, `int64Ptr`, `stampsFromMap`, `mapFromStamps`, `detailedObjectType`.
- Produces: the `accumulator_set` schema gains `expires_after` and `detailed_outputs`; `setResourceModel` gains `ExpiresAfter types.Int64` and `DetailedOutputs types.Map`. The shared test helpers `nullDetail`, `stampedDetail`, `expectDetailed`, `expectPlanDetailed`, `sleepPastExpiry` in `provider_test.go` are reused by Task 4.

- [ ] **Step 1: Extend the model and schema**

In `resource_set.go`, extend `setResourceModel`:

```go
// setResourceModel is accumulator_set's state model.
type setResourceModel struct {
	Inputs              types.List   `tfsdk:"inputs"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	Outputs             types.Set    `tfsdk:"outputs"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
	ID                  types.String `tfsdk:"id"`
}
```

Add two entries to the schema's `Attributes` map (after `triggers_replacement`, before `outputs`), and add imports `"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"` and `"github.com/hashicorp/terraform-plugin-framework/schema/validator"`:

```go
			"expires_after": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
				MarkdownDescription: "How many seconds a value that has left `inputs` survives in " +
					"`outputs`. While a value stays in `inputs` it never expires and its `expires_at` " +
					"is null. Expiration is lazy: a value is removed only when a plan/apply runs after " +
					"its `expires_at` has passed. Plan times are used: `expires_at` is stamped with the " +
					"clock of the plan that computes it, and a late-applied plan will not expire an item " +
					"even if the time has passed by the time the plan is applied — a new plan/apply is " +
					"required. Changing the value recalculates every existing `expires_at` as " +
					"min(old, new) on a decrease and max(old, new) on an increase. Must be zero or " +
					"greater; `0` removes a value at the same plan where it leaves `inputs`. Leave it " +
					"null to disable expiration entirely.",
			},
			"detailed_outputs": schema.MapAttribute{
				Computed:    true,
				ElementType: detailedObjectType,
				MarkdownDescription: "A map from every value in `outputs` to its expiration attributes. " +
					"The single attribute, `expires_at`, is an RFC 3339 timestamp of when the value " +
					"will be removed, or null when the value cannot expire: it is in `inputs`, or " +
					"`expires_after` is null. Computed; never configured. The plan shows the value the " +
					"next apply will produce, computed against the prior state with the plan clock; " +
					"when `inputs`, a trigger, or `expires_after` is unknown at plan time, this " +
					"attribute plans as unknown and the apply resolves it. Plan times are used: a " +
					"late-applied plan will not expire an item whose time has passed after the plan " +
					"was created — a new plan/apply is required.",
			},
```

Extend the resource-level `MarkdownDescription` by appending one sentence after the existing text:

```go
			"ordering of its elements; only the membership is meaningful. " +
			"With `expires_after` set, values that leave `inputs` expire after the TTL; see " +
			"`expires_after` for the plan-clock rules.",
```

- [ ] **Step 2: Rewrite Create, Update, ModifyPlan, ImportState**

Replace `Create` wholesale:

```go
// Create seeds outputs from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
//
// Known path: ModifyPlan already computed outputs, detailed_outputs, and
// the id with the plan clock (spec section 6); realizing the plan verbatim
// is what keeps the applied value identical to the planned one. Fallback
// path (unknown inputs at plan time): create is the reseed case, every
// value comes from inputs, so ApplyExpiration stamps nothing and the clock
// never participates; it is still run so both paths shape the result the
// same way.
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

	if !plan.Outputs.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextSetOutputs(true, inputs, nil)
	survivors, stamps := accumulate.ApplyExpiration(merged, inputs, nil, nil,
		int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

Replace `Update` wholesale:

```go
// Update realizes the plan. On the known path every time-dependent decision
// was already made by ModifyPlan with the plan clock and recorded in the
// plan, so Update copies it verbatim (spec section 6). The fallback path
// runs only when the plan is unknown (unknown branch inputs at plan time),
// where the plan promised unknown and Update is free to decide with the
// apply clock. No inputs comparison is needed: union is idempotent.
func (r *setResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Outputs.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	stateOutputs, d := stringsFromSet(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	priorStamps, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	reseed := !plan.TriggersReset.Equal(state.TriggersReset)
	merged := accumulate.NextSetOutputs(reseed, planInputs, stateOutputs)
	survivors, stamps := accumulate.ApplyExpiration(merged, planInputs, priorStamps,
		int64Ptr(state.ExpiresAfter), int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

In `ModifyPlan`, make four changes. First, extend the state-reading block (after `var state setResourceModel`) so prior expiration data is available:

```go
	var state setResourceModel
	var stateOutputs []string
	var priorStamps map[string]time.Time
	var priorExpiresAfter *int64
	if !creating {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
```

Second, extend both unknown guards (`plan.ExpiresAfter.IsUnknown()` joins the plan guard; `state.ExpiresAfter.IsUnknown()` and `state.DetailedOutputs.IsUnknown()` join the state guard), keeping the existing comment intact:

```go
	if listContainsUnknown(plan.Inputs) || plan.TriggersReset.IsUnknown() ||
		plan.TriggersReplacement.IsUnknown() || plan.ExpiresAfter.IsUnknown() {
		return
	}
	if !creating && (state.Inputs.IsUnknown() || state.Outputs.IsUnknown() ||
		state.TriggersReset.IsUnknown() || state.TriggersReplacement.IsUnknown() ||
		state.ExpiresAfter.IsUnknown() || state.DetailedOutputs.IsUnknown()) {
		return
	}
```

Third, fill the prior data where `stateOutputs` is read today:

```go
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if !creating {
		stateOutputs, d = stringsFromSet(ctx, state.Outputs)
		resp.Diagnostics.Append(d...)
		priorStamps, d = stampsFromMap(ctx, state.DetailedOutputs)
		resp.Diagnostics.Append(d...)
		priorExpiresAfter = int64Ptr(state.ExpiresAfter)
	}
	if resp.Diagnostics.HasError() {
		return
	}
```

Fourth, replace the tail that computes and writes outputs:

```go
	replacing := !creating && !plan.TriggersReplacement.Equal(state.TriggersReplacement)
	reseed := creating || replacing || !plan.TriggersReset.Equal(state.TriggersReset)

	merged := accumulate.NextSetOutputs(reseed, planInputs, stateOutputs)

	// Expiration runs after the accumulation (spec section 7): union first,
	// then filter, so expiration can never resurrect or pre-empt the
	// accumulation. This is the plan clock, the only clock on this path;
	// Update copies these values verbatim.
	survivors, stamps := accumulate.ApplyExpiration(merged, planInputs, priorStamps,
		priorExpiresAfter, int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	if creating || replacing {
		// Create identifies the resource from the planned inputs. An in-place
		// update keeps the state id; UseStateForUnknown has already resolved
		// it into the plan by the time ModifyPlan runs.
		plan.ID = types.StringValue(accumulate.HashID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
```

Add the `"time"` import to `resource_set.go` (used by the `priorStamps` declaration).

Finally, in `ImportState`, seed the new attributes (build `detailed` after `merged`, before the final `State.Set`, and add both fields to the model literal):

```go
	detailed, d := mapFromStamps(ctx, merged, nil)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &setResourceModel{
		Inputs:              inputs,
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		Outputs:             outputs,
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(accumulate.HashID(merged)),
	})...)
```

- [ ] **Step 3: Add the shared acceptance helpers**

Append to `internal/provider/provider_test.go`, adding `"regexp"` and `"time"` to its imports:

```go
// nullDetail is the knownvalue check for one detailed_outputs entry whose
// expires_at is null: the value cannot expire.
func nullDetail() knownvalue.Check {
	return knownvalue.ObjectExact(map[string]knownvalue.Check{
		"expires_at": knownvalue.NullExact(),
	})
}

// stampedDetail matches one detailed_outputs entry whose expires_at is a
// non-null RFC 3339 UTC timestamp. The concrete instant comes from the plan
// clock, so acceptance tests match the shape, not the time.
func stampedDetail() knownvalue.Check {
	return knownvalue.ObjectExact(map[string]knownvalue.Check{
		"expires_at": knownvalue.StringRegexp(regexp.MustCompile(
			`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)),
	})
}

// expectDetailed checks the whole detailed_outputs value in state.
func expectDetailed(addr string, entries map[string]knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New("detailed_outputs"),
		knownvalue.MapExact(entries))
}

// expectPlanDetailed checks the planned detailed_outputs is known and
// matches: the plan must show the map the apply will produce, not "(known
// after apply)".
func expectPlanDetailed(addr string, entries map[string]knownvalue.Check) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue(addr, tfjsonpath.New("detailed_outputs"),
		knownvalue.MapExact(entries))
}

// sleepPastExpiry is a TestStep.PreConfig that waits out the short
// expires_after values the expiration tests use (1s), so the next step's
// plan sees the value as expired.
func sleepPastExpiry() func() {
	return func() { time.Sleep(2 * time.Second) }
}

// setDetail builds a one-entry map for expectDetailed/expectPlanDetailed.
func setDetail(value string, check knownvalue.Check) map[string]knownvalue.Check {
	return map[string]knownvalue.Check{value: check}
}
```

- [ ] **Step 4: Write the set acceptance tests**

Append to `internal/provider/resource_set_test.go`. All use the existing `setConfig(inputs, extra)` helper — `expires_after` rides in `extra`. A long `expires_after` (3600) keeps stamps far in the future so `expectEmptyAfterRefresh` is never racing a clock; steps that deliberately leave a `1s` stamp pending say so and skip the post-refresh check.

```go
// TestAccSetCreateWithExpiresAfter pins the create shape: every value is in
// inputs, so detailed_outputs is fully known at plan time and all null.
func TestAccSetCreateWithExpiresAfter(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: setConfig(`["a", "b"]`, `  expires_after = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectSetPlanOutputs("a", "b"),
					expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs("a", "b"),
				expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
					"a": nullDetail(), "b": nullDetail(),
				}),
			},
		}},
	})
}

// TestAccSetLeaverStampedOnInputsChange pins S1: b leaves inputs and is
// stamped by the plan clock; a stays null. The 3600s window keeps the
// post-refresh plan empty without racing the clock.
func TestAccSetLeaverStampedOnInputsChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": stampedDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetExpiryRealizedOnBareApply pins the forced-update mechanic: with
// no configuration change, the fresh plan shows b's removal as an update and
// the apply realizes it. Step 2 deliberately leaves a 1s stamp pending, so
// it carries no post-refresh check; step 3 sleeps past the stamp and re-applies
// the same config.
func TestAccSetExpiryRealizedOnBareApply(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", setDetail("b", stampedDetail())),
				},
			},
			{
				Config:             setConfig(`["a"]`, `  expires_after = 1`),
				PreConfig:          sleepPastExpiry(),
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a"),
						expectPlanDetailed("accumulator_set.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetInInputsNeverExpires pins R2 and the steady-state invariant: with
// expires_after = 1 held over a sleep, a stays unstamped, unremoved, and the
// plan after the sleep is empty (invariant 1 holds with expires_after set).
func TestAccSetInInputsNeverExpires(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
			{
				Config:    setConfig(`["a"]`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterSetOnExistingResource pins S5: history that
// accumulated with no expires_after gets a full window when it is enabled.
func TestAccSetExpiresAfterSetOnExistingResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a", "b"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterRemoved pins T2: removing expires_after expires
// nothing and nulls every expires_at.
func TestAccSetExpiresAfterRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// T2 removes nothing: b survives the removal of
						// expires_after, with its stamp nulled.
						expectSetPlanOutputs("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterZeroRemovesLeaverImmediately pins X1's E=0 case:
// dropping expires_after to 0 removes b at the same plan.
func TestAccSetExpiresAfterZeroRemovesLeaverImmediately(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetExpiresAfterDecreasePullsEarlier pins S3 end to end: b was
// stamped +1h; decreasing to 1 recalculates min(old, ~now+1s), and the
// following bare apply removes it. Step 3 leaves a 1s stamp pending, so it
// carries no post-refresh check.
func TestAccSetExpiresAfterDecreasePullsEarlier(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", setDetail("b", stampedDetail())),
				},
			},
			{
				Config:             setConfig(`["a"]`, `  expires_after = 1`),
				PreConfig:          sleepPastExpiry(),
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectSetPlanOutputs("a")},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
				},
			},
		},
	})
}

// TestAccSetIncreaseRescuesExpiredUnrealized pins S4 plus X1: b's 1s window
// has passed by step 3's plan, but raising expires_after in that same plan
// recalculates max(old, ~now+1h) and b survives. Step 2 leaves the 1s stamp
// pending, so it carries no post-refresh check.
func TestAccSetIncreaseRescuesExpiredUnrealized(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:    setConfig(`["a"]`, `  expires_after = 3600`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": stampedDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetReSupplyRescues pins T5: b's 1s window has passed unrealized,
// and re-adding b to inputs rescues it with a null stamp. Step 2 leaves the
// 1s stamp pending, so it carries no post-refresh check.
func TestAccSetReSupplyRescues(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:    setConfig(`["a", "b"]`, `  expires_after = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetResetClearsStamps pins T4: a reset reseeds from inputs alone, so
// detailed_outputs is all null again.
func TestAccSetResetClearsStamps(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600
  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600
  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600
  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a"),
						expectPlanDetailed("accumulator_set.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetReplacementClearsStamps pins T4's replacement half: a
// triggers_replacement change destroys and recreates, Create reseeds from
// inputs alone, and detailed_outputs is all null again.
func TestAccSetReplacementClearsStamps(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  expires_after = 3600
  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config:            setConfig(`["a"]`, `  expires_after = 3600
  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
			{
				Config: setConfig(`["a"]`, `  expires_after = 3600
  triggers_replacement = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_set.test", plancheck.ResourceActionReplace),
						expectSetPlanOutputs("a"),
						expectPlanDetailed("accumulator_set.test", setDetail("a", nullDetail())),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetUnknownExpiresAfterAtPlan pins the fallback path: expires_after
// derived from an upstream unknown stays unknown at plan, so outputs and
// detailed_outputs plan as unknown and the apply resolves them with the
// apply clock. The upstream resource diffs forever (uuid), so there is no
// post-refresh check, matching TestAccSetOutputsUnknownWhenInputsUnknown.
func TestAccSetUnknownExpiresAfterAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_set" "test" {
  inputs         = ["a"]
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
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs("a"),
				expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
			},
		}},
	})
}

// TestAccSetImportThenExpiresAfter pins spec section 8: the import ID
// carries no expiration data, imported entries are null, and enabling
// expires_after in the settling apply stamps non-inputs values via S5.
func TestAccSetImportThenExpiresAfter(t *testing.T) {
	config := `
resource "accumulator_set" "test" {
  inputs        = ["a"]
  expires_after = 3600
}
`
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
			},
			{
				// The settling apply: a is in inputs (null), b is seeded
				// history with no prior stamp, so S5 stamps it.
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": stampedDetail(),
					}),
				},
			},
		},
	})
}
```

- [ ] **Step 5: Run the acceptance tests**

```bash
make testacc TESTARGS='-run "TestAccSet(CreateWithExpiresAfter|LeaverStamped|ExpiryRealized|InInputsNeverExpires|ExpiresAfterSetOnExisting|ExpiresAfterRemoved|ExpiresAfterZero|ExpiresAfterDecrease|IncreaseRescues|ReSupplyRescues|ResetClearsStamps|ReplacementClearsStamps|UnknownExpiresAfter|ImportThenExpiresAfter)"'
```

Expected: PASS. If `TestAccSetExpiryRealizedOnBareApply` fails with "After applying this test step, the plan was not empty" style errors on step 3's `PreApply` checks, the forced-update mechanic is not engaging: confirm ModifyPlan is writing the removal into the plan (known computed values differing from state drive Update). If `TestProviderSchema` fails, the schema change has an implementation error; its diagnostic names it.

- [ ] **Step 6: House checks and commit**

```bash
gofmt -l . && go vet ./... && go test ./internal/...
git add internal/provider/resource_set.go internal/provider/provider_test.go internal/provider/resource_set_test.go
git commit -m "feat: accumulator_set expires_after and detailed_outputs"
```

`go test ./internal/...` now also runs `TestStateGetToleratesMissingExpirationAttributes` for real (the set schema carries the new attributes; the list case still needs Task 4 and skips).

---

### Task 4: `accumulator_list` wiring and acceptance tests

> **SUPERSEDED in part (2026-09-24):** Step 2's Create/Update/ModifyPlan/ImportState code follows the same plan-time-clock mechanics that failed on OpenTofu's double-plan check. Apply the "Revision 2026-09-24 — refresh-culling rework" section at the end of this document to every resource method it names (it covers both resources); the acceptance tests it does not rework stand as written here.

The same wiring for the list resource, with `length` and `inputsChanged` in the branch decision, plus the duplicates-expire-together behavior that is unique to it.

**Files:**
- Modify: `internal/provider/resource_list.go`
- Test: `internal/provider/resource_list_test.go`
- Modify: `internal/provider/import_test.go` (one added assertion)

**Interfaces:**
- Consumes: `accumulate.ApplyExpiration`, `clockNow`, `int64Ptr`, `stampsFromMap`, `mapFromStamps`, `detailedObjectType`, and the Task 3 test helpers.
- Produces: the `accumulator_list` schema gains `expires_after` and `detailed_outputs`; `listResourceModel` gains `ExpiresAfter types.Int64` and `DetailedOutputs types.Map`.

- [ ] **Step 1: Extend the model and schema**

Extend `listResourceModel`:

```go
// listResourceModel is accumulator_list's state model.
type listResourceModel struct {
	Inputs              types.List   `tfsdk:"inputs"`
	Length              types.Int64  `tfsdk:"length"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	ExpiresAfter        types.Int64  `tfsdk:"expires_after"`
	Outputs             types.List  `tfsdk:"outputs"`
	DetailedOutputs     types.Map    `tfsdk:"detailed_outputs"`
	ID                  types.String `tfsdk:"id"`
}
```

Add the same two attribute entries to the schema as Task 3 Step 1 (verbatim, including the MarkdownDescriptions), and append one sentence to the resource-level `MarkdownDescription`:

```go
			"before: there is no deduplication here. " +
			"With `expires_after` set, values that leave `inputs` expire after the TTL; see " +
			"`expires_after` for the plan-clock rules.",
```

- [ ] **Step 2: Rewrite Create, Update, ModifyPlan, ImportState**

Replace `Create` wholesale:

```go
// Create seeds outputs from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
//
// Known path: ModifyPlan already computed outputs, detailed_outputs, and
// the id with the plan clock (spec section 6); realize it verbatim.
// Fallback path (unknown inputs at plan time): create is the reseed case,
// every value comes from inputs, so ApplyExpiration stamps nothing and the
// clock never participates; it is still run so both paths shape the result
// the same way.
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

	if !plan.Outputs.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextListOutputs(true, false, inputs, nil, int(plan.Length.ValueInt64()))
	survivors, stamps := accumulate.ApplyExpiration(merged, inputs, nil, nil,
		int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := listFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

Replace `Update` wholesale:

```go
// Update realizes the plan. On the known path every time-dependent decision
// was already made by ModifyPlan with the plan clock and recorded in the
// plan, so Update copies it verbatim (spec section 6). The fallback path
// runs only when the plan is unknown (unknown branch inputs at plan time).
//
// triggers_replacement is never inspected here: a change to it plans a
// replacement, and Update is not called.
func (r *listResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Outputs.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	stateOutputs, d := stringsFromList(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	priorStamps, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	length := int(plan.Length.ValueInt64())

	// The branch order is pinned by accumulate.NextListOutputs: reset wins
	// over an inputs change, and an inputs change wins over a length-only
	// re-trim. Expiration filters afterward (spec section 7): trim first,
	// then expire, so expiration can never resurrect a trimmed value.
	reseed := !plan.TriggersReset.Equal(state.TriggersReset)
	inputsChanged := !plan.Inputs.Equal(state.Inputs)
	merged := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, stateOutputs, length)
	survivors, stamps := accumulate.ApplyExpiration(merged, planInputs, priorStamps,
		int64Ptr(state.ExpiresAfter), int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := listFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

In `ModifyPlan`, make the same four changes as Task 3 Step 2, with `length`/`inputsChanged` in place. The extended state-reading block:

```go
	var state listResourceModel
	var stateOutputs []string
	var priorStamps map[string]time.Time
	var priorExpiresAfter *int64
	if !creating {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
```

The extended guards (add `plan.Length.IsUnknown()` already exists; add `plan.ExpiresAfter.IsUnknown()` to the plan guard, and `state.ExpiresAfter.IsUnknown() || state.DetailedOutputs.IsUnknown()` to the state guard):

```go
	if listContainsUnknown(plan.Inputs) || plan.Length.IsUnknown() ||
		plan.TriggersReset.IsUnknown() || plan.TriggersReplacement.IsUnknown() ||
		plan.ExpiresAfter.IsUnknown() {
		return
	}
	if !creating && (state.Inputs.IsUnknown() || state.Outputs.IsUnknown() ||
		state.TriggersReset.IsUnknown() || state.TriggersReplacement.IsUnknown() ||
		state.ExpiresAfter.IsUnknown() || state.DetailedOutputs.IsUnknown()) {
		return
	}
```

The prior-data fill:

```go
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if !creating {
		stateOutputs, d = stringsFromList(ctx, state.Outputs)
		resp.Diagnostics.Append(d...)
		priorStamps, d = stampsFromMap(ctx, state.DetailedOutputs)
		resp.Diagnostics.Append(d...)
		priorExpiresAfter = int64Ptr(state.ExpiresAfter)
	}
	if resp.Diagnostics.HasError() {
		return
	}
```

And the tail (replacing the block that computes and writes outputs):

```go
	replacing := !creating && !plan.TriggersReplacement.Equal(state.TriggersReplacement)
	reseed := creating || replacing || !plan.TriggersReset.Equal(state.TriggersReset)
	inputsChanged := !creating && !plan.Inputs.Equal(state.Inputs)

	merged := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, stateOutputs,
		int(plan.Length.ValueInt64()))

	// Expiration runs after the accumulation and trim (spec section 7).
	// This is the plan clock, the only clock on this path; Update copies
	// these values verbatim.
	survivors, stamps := accumulate.ApplyExpiration(merged, planInputs, priorStamps,
		priorExpiresAfter, int64Ptr(plan.ExpiresAfter), clockNow())

	out, d := listFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, stamps)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	if creating || replacing {
		// Create identifies the resource from the planned inputs. An in-place
		// update keeps the state id; UseStateForUnknown has already resolved
		// it into the plan by the time ModifyPlan runs.
		plan.ID = types.StringValue(accumulate.HashID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
```

Add the `"time"` import to `resource_list.go`.

In `ImportState`, seed the new attributes (build `detailed` from `seed.Outputs`, which `mapFromStamps` deduplicates by map key, and add both fields to the model literal):

```go
	detailed, d := mapFromStamps(ctx, seed.Outputs, nil)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &listResourceModel{
		Inputs:              inputs,
		Length:              types.Int64Null(),
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		ExpiresAfter:        types.Int64Null(),
		Outputs:             outputs,
		DetailedOutputs:     detailed,
		ID:                  types.StringValue(accumulate.HashID(seed.Inputs)),
	})...)
```

- [ ] **Step 3: Write the list acceptance tests**

Append to `internal/provider/resource_list_test.go`, using the existing `listConfig(inputs, length, extra)` helper and the Task 3 shared helpers:

```go
// TestAccListCreateWithExpiresAfter pins the create shape for the list
// resource: all values in inputs, detailed_outputs fully known and null.
func TestAccListCreateWithExpiresAfter(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(`["a"]`, 5, `  expires_after = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectListPlanOutputs("a"),
					expectPlanDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs("a"),
				expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
			},
		}},
	})
}

// TestAccListLeaverStampedOnInputsChange pins S1 for the list: a and b leave
// inputs when c arrives, both are stamped, c is null, and the accumulated
// order is preserved.
func TestAccListLeaverStampedOnInputsChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config: listConfig(`["c"]`, 10, `  expires_after = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "b", "c"),
						expectPlanDetailed("accumulator_list.test", map[string]knownvalue.Check{
							"a": stampedDetail(), "b": stampedDetail(), "c": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "c"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": stampedDetail(), "b": stampedDetail(), "c": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListExpiryRealizedOnBareApply pins the forced-update mechanic for
// the list resource. Step 2's inputs change [a] -> [b] appends b (list
// semantics: the whole new list is appended) and leaves a with a pending 1s
// stamp, so it carries no post-refresh check; step 3 sleeps past the stamp
// and re-applies the same config.
func TestAccListExpiryRealizedOnBareApply(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
			{
				Config: listConfig(`["b"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": stampedDetail(), "b": nullDetail(),
					}),
				},
			},
			{
				Config:             listConfig(`["b"]`, 10, `  expires_after = 1`),
				PreConfig:          sleepPastExpiry(),
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("b"),
					expectDetailed("accumulator_list.test", setDetail("b", nullDetail())),
				},
			},
		},
	})
}

// TestAccListDuplicatesExpireTogether pins the duplicate rule: a appears
// twice, both occurrences share one detailed_outputs entry, and both are
// removed together once a's stamp passes. Step 2's inputs change appends b
// again (list semantics), and leaves a's 1s stamp pending, so it carries no
// post-refresh check.
func TestAccListDuplicatesExpireTogether(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "a", "b"]`, 10, `  expires_after = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "a", "b")},
			},
			{
				Config: listConfig(`["b"]`, 10, `  expires_after = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					// [a, a, b] ++ [b]: the list has no deduplication.
					expectListOutputs("a", "a", "b", "b"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": stampedDetail(), "b": nullDetail(),
					}),
				},
			},
			{
				Config:             listConfig(`["b"]`, 10, `  expires_after = 1`),
				PreConfig:          sleepPastExpiry(),
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{expectListPlanOutputs("b", "b")},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("b", "b"),
					expectDetailed("accumulator_list.test", setDetail("b", nullDetail())),
				},
			},
		},
	})
}
```

- [ ] **Step 4: Add the import assertion**

In `internal/provider/import_test.go`, extend `TestAccListImport`'s settling step (the second `resource.TestStep`, around line 70-79) with one state check:

```go
			{
				// The settling apply. length goes null -> 3 and outputs is
				// re-trimmed from the seeded value; after that the plan is
				// empty. Import seeds all-null detailed_outputs (spec
				// section 8), which is what keeps this plan empty.
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "c"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(), "c": nullDetail(),
					}),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
```

- [ ] **Step 5: Run the acceptance tests**

```bash
make testacc TESTARGS='-run "TestAccList(CreateWithExpiresAfter|LeaverStamped|ExpiryRealized|DuplicatesExpireTogether|Import)"'
```

Expected: PASS, including the pre-existing `TestAccListImport`.

- [ ] **Step 6: Full suite, house checks, commit**

```bash
make testacc
gofmt -l . && go vet ./... && go test ./internal/...
git add internal/provider/resource_list.go internal/provider/resource_list_test.go internal/provider/import_test.go
git commit -m "feat: accumulator_list expires_after and detailed_outputs"
```

The full `make testacc` run is the whole-suite regression: every pre-existing test must stay green (they pin the E-null behavior, which must be identical to today's).

---

### Task 5: Examples and generated documentation

The schema descriptions landed in Tasks 3 and 4; this task surfaces them in the registry docs and examples, and satisfies the spec's section 11 requirement that the plan-time rule and the late-applied-plan caveat appear in the documentation (they are in the attribute descriptions verbatim).

**Files:**
- Modify: `examples/resources/accumulator_set/resource.tf`
- Modify: `examples/resources/accumulator_list/resource.tf`
- Regenerate: `docs/index.md`, `docs/resources/list.md`, `docs/resources/set.md` (via `make docs`)

**Interfaces:**
- Consumes: the finished schemas from Tasks 3 and 4.
- Produces: regenerated docs; nothing downstream.

- [ ] **Step 1: Update the examples**

Extend the existing examples in place, keeping their resource and variable names. Replace `examples/resources/accumulator_set/resource.tf` with:

```hcl
resource "accumulator_set" "seen_hosts" {
  inputs        = [var.hostname]
  expires_after = 3600
}

output "seen_hosts" {
  value = accumulator_set.seen_hosts.outputs
}

output "seen_hosts_detail" {
  value = accumulator_set.seen_hosts.detailed_outputs
}
```

Replace `examples/resources/accumulator_list/resource.tf` with:

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs        = [var.deploy_sha]
  length        = 5
  expires_after = 86400
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}

output "recent_deploys_detail" {
  value = accumulator_list.recent_deploys.detailed_outputs
}
```

- [ ] **Step 2: Regenerate the docs and verify the tree**

```bash
make docs
git status --short
git diff --stat
```

Expected: `docs/index.md`, `docs/resources/list.md`, `docs/resources/set.md` change; `docs/superpowers/` is untouched. Spot-check that `docs/resources/set.md` documents both new attributes with the plan-time sentences and shows the updated example.

- [ ] **Step 3: Final full verification**

```bash
make testacc
gofmt -l . && go vet ./... && go test ./internal/...
```

Expected: everything green.

- [ ] **Step 4: Commit**

```bash
git add examples/resources/accumulator_set/resource.tf examples/resources/accumulator_list/resource.tf docs/index.md docs/resources/list.md docs/resources/set.md
git commit -m "docs: regenerate for expires_after and detailed_outputs"
```

---

## Self-Review Notes

- **Spec coverage:** every rule in spec section 4 maps to a task: R1 (Tasks 3/4 keep `Next*Outputs` first), R2/S1/S2/S3/S4/S5/X1/T1-T5 (Task 1 table plus Tasks 3/4 acceptance tests), schema (Tasks 3/4), plan/apply mechanics (Tasks 3/4), pure function (Task 1), import (Tasks 3/4 seeding plus import tests), state compatibility (Task 2), testing (all tasks), documentation (Tasks 3/4 descriptions + Task 5 regeneration), limitations (documented in spec, not code).
- **Type consistency:** `ApplyExpiration`'s signature is identical in Tasks 1, 3, and 4; helper names (`clockNow`, `int64Ptr`, `stampsFromMap`, `mapFromStamps`, `detailedObjectType`, `nullDetail`, `stampedDetail`, `expectDetailed`, `expectPlanDetailed`, `sleepPastExpiry`, `setDetail`) are defined exactly once and consumed with those names.
- **Known execution risk:** the two `ExpectNonEmptyPlan` bare-apply tests (`TestAccSetExpiryRealizedOnBareApply` step 3, `TestAccListExpiryRealizedOnBareApply` step 3, `TestAccSetExpiresAfterDecreasePullsEarlier` step 4, `TestAccListDuplicatesExpireTogether` step 3) verify that OpenTofu treats a computed-attribute plan diff as an update and calls Update. That is the framework behavior the design depends on; if one of these fails, debug ModifyPlan's written plan values first, not the sleeps.
- **Timing margins:** stamps are second-truncated by `clockNow`; `sleepPastExpiry` sleeps 2s against 1s windows; `expires_after = 3600` keeps every `expectEmptyAfterRefresh` far from the clock. Steps that end with a pending 1s stamp deliberately omit post-refresh checks and say so in a comment.

---

## Revision 2026-09-24 — refresh-culling rework

This section supersedes the parts of Tasks 3 and 4 named below. The approved spec was revised the same day (see the revision note at the top of `docs/superpowers/specs/2026-09-23-accumulator-expiration-design.md`); this section implements that revision.

**Why.** OpenTofu calls `PlanResourceChange`/ModifyPlan twice per apply — once for the saved plan, once during apply to derive the final planned state — and requires every value known in the first plan to be identical in the second. The Task 3 body's ModifyPlan wrote `clockNow()`-derived timestamps as known planned values, so whenever the two invocations straddled a second boundary, applies failed with `Provider produced inconsistent final plan` (reproduced deterministically with `tofu plan -out` plus a delayed `tofu apply`). Following `hashicorp/terraform-provider-time`'s `time_rotating` prior art, the clock now lives in exactly two places: **Read** (refresh culling) and **Create/Update when stamping fresh entries** (born at apply, unknown at plan). ModifyPlan never reads the clock.

### R.1 Pure functions (`internal/accumulate/expire.go`)

Add three functions and refactor `ApplyExpiration` to delegate to them. **Every existing `ApplyExpiration` test case must stay green unchanged** — the refactor preserves behavior exactly.

```go
// FilterExpired drops values whose stored stamp is not strictly after now
// (spec X1, the refresh rule). Values without a stamp are never dropped.
// Read calls it with the refresh clock; the Update fallback with the apply
// clock. Both returns are non-nil, and the returned stamp map only holds
// survivors' stamps.
func FilterExpired(values []string, stamps map[string]time.Time, now time.Time) ([]string, map[string]time.Time) {
	survivors := make([]string, 0, len(values))
	kept := make(map[string]time.Time, len(stamps))
	for _, v := range values {
		stamp, ok := stamps[v]
		if ok && !stamp.After(now) {
			continue
		}
		survivors = append(survivors, v)
		if ok {
			kept[v] = stamp
		}
	}
	return survivors, kept
}

// ClassifyStamps partitions the stampable survivors — values in survivors
// that are not in planInputs — into keep and fresh. keep holds prior
// stamps that survive unchanged: plannedExpiresAfter is set, equals
// priorExpiresAfter, and the value has a prior stamp (S2). fresh holds
// every other stampable value when plannedExpiresAfter is set: no prior
// stamp (S1, S5), no prior expiresAfter (S5), or a changed
// expiresAfter (S3, S4). Values in planInputs, and everything under a
// null plannedExpiresAfter, belong to neither: their expires_at is null.
// It reads no clock; ModifyPlan uses it to decide which detailed_outputs
// entries are known and which stay unknown until the apply stamps them.
func ClassifyStamps(survivors, planInputs []string,
	prior map[string]time.Time,
	priorExpiresAfter, plannedExpiresAfter *int64,
) (keep map[string]time.Time, fresh map[string]struct{}) {
	keep = make(map[string]time.Time)
	fresh = make(map[string]struct{})
	if plannedExpiresAfter == nil {
		return keep, fresh
	}
	supplied := make(map[string]struct{}, len(planInputs))
	for _, v := range planInputs {
		supplied[v] = struct{}{}
	}
	for _, v := range survivors {
		if _, ok := supplied[v]; ok {
			continue
		}
		stamp, had := prior[v]
		unchanged := priorExpiresAfter != nil && *priorExpiresAfter == *plannedExpiresAfter
		if had && unchanged {
			keep[v] = stamp
			continue
		}
		fresh[v] = struct{}{}
	}
	return keep, fresh
}

// FreshStamp computes one fresh value's apply-time stamp: the candidate
// now + plannedExpiresAfter, kept as min(old, candidate) on a decrease and
// max(old, candidate) on an increase when a prior stamp exists (S1, S3,
// S4, S5).
func FreshStamp(prior map[string]time.Time,
	priorExpiresAfter, plannedExpiresAfter *int64,
	now time.Time, value string) time.Time {
	candidate := now.Add(time.Duration(*plannedExpiresAfter) * time.Second)
	stamp, had := prior[value]
	switch {
	case !had || priorExpiresAfter == nil:
		return candidate
	case *priorExpiresAfter > *plannedExpiresAfter:
		if candidate.Before(stamp) {
			return candidate
		}
	case *priorExpiresAfter < *plannedExpiresAfter:
		if candidate.After(stamp) {
			return candidate
		}
	}
	return stamp
}
```

`ApplyExpiration` keeps its signature and semantics (the Update fallback path still uses it) but its per-value switch moves into `FreshStamp`:

```go
func ApplyExpiration(result, planInputs []string,
	prior map[string]time.Time,
	priorExpiresAfter, plannedExpiresAfter *int64,
	now time.Time) ([]string, map[string]time.Time) {

	survivors := make([]string, 0, len(result))
	stamps := make(map[string]time.Time, len(result))
	if plannedExpiresAfter == nil {
		return append(survivors, result...), stamps
	}

	supplied := make(map[string]struct{}, len(planInputs))
	for _, v := range planInputs {
		supplied[v] = struct{}{}
	}

	for _, v := range result {
		if _, ok := supplied[v]; ok {
			survivors = append(survivors, v)
			continue
		}
		stamp := FreshStamp(prior, priorExpiresAfter, plannedExpiresAfter, now, v)
		if !stamp.After(now) {
			continue
		}
		survivors = append(survivors, v)
		stamps[v] = stamp
	}
	return survivors, stamps
}
```

New table-driven unit tests in `expire_test.go` for `FilterExpired` (drop at `stamp == now` inclusive; keep future stamps; never drop unstamped values; nil map; empty input), `ClassifyStamps` (keeper vs fresh for every S-rule shape; in-inputs and null-E values in neither; nil prior), and `FreshStamp` (each arm of the switch, mirroring the existing min/max table rows).

### R.2 Provider helper (`internal/provider/model.go`)

```go
// classifiedMap builds detailed_outputs for the plan: a null expires_at
// for values with no stamp, the formatted prior stamp for keepers (S2),
// and an unknown expires_at for fresh values the apply will stamp (S1,
// S3, S4, S5). The map keys stay known; only the fresh entries' attribute
// is unknown. It reads no clock.
func classifiedMap(ctx context.Context, survivors []string, keep map[string]time.Time, fresh map[string]struct{}) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(survivors))
	for _, v := range survivors {
		entry := detailAttr{ExpiresAt: types.StringNull()}
		if _, ok := fresh[v]; ok {
			entry.ExpiresAt = types.StringUnknown()
		} else if stamp, ok := keep[v]; ok {
			entry.ExpiresAt = types.StringValue(stamp.UTC().Format(time.RFC3339))
		}
		entries[v] = entry
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}
```

### R.3 Read replaces the no-op (both resources)

```go
// Read culls expired values: the refresh-time half of the expiration
// design (spec section 6), and the per-value analogue of time_rotating's
// Read removing the resource once its rotation deadline passes. Read
// cannot see configuration, so the cull runs on stored stamps alone;
// values in inputs are safe because they never carry a stamp (R2). When
// nothing expired, the method returns without touching the response,
// which keeps the old no-op behavior for everything else.
func (r *setResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state setResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values, d := stringsFromSet(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	stamps, d := stampsFromMap(ctx, state.DetailedOutputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	survivors, kept := accumulate.FilterExpired(values, stamps, clockNow())
	if len(survivors) == len(values) {
		return
	}

	out, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	detailed, d := mapFromStamps(ctx, survivors, kept)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Outputs = out
	state.DetailedOutputs = detailed
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
```

`accumulator_list` is identical with `stringsFromList`/`listFromStrings` in place of the set converters. Update both resources' Read doc comments accordingly; the old "refresh can never perturb an attribute" wording becomes "refresh only ever removes expired values, and only when at least one is expired".

### R.4 ModifyPlan tail (both resources)

Replace the Task 3 Step 2 fourth-change block (the one calling `ApplyExpiration` with `clockNow()`) with a clock-free classification. Set version:

```go
	merged := accumulate.NextSetOutputs(reseed, planInputs, stateOutputs)

	// No clock here (spec section 6): refresh already culled expired
	// values, so membership is a pure function of state and configuration,
	// and OpenTofu's apply-time re-derivation of this plan reproduces it
	// exactly. ClassifyStamps decides which detailed_outputs entries are
	// known (prior stamps under an unchanged expires_after) and which stay
	// unknown until the apply stamps them (S1, S3, S4, S5).
	keep, fresh := accumulate.ClassifyStamps(merged, planInputs, priorStamps,
		priorExpiresAfter, int64Ptr(plan.ExpiresAfter))

	out, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	detailed, d := classifiedMap(ctx, merged, keep, fresh)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.DetailedOutputs = detailed
	if creating || replacing {
		// Create identifies the resource from the planned inputs. An in-place
		// update keeps the state id; UseStateForUnknown has already resolved
		// it into the plan by the time ModifyPlan runs.
		plan.ID = types.StringValue(accumulate.HashID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
```

The list version passes `reseed, inputsChanged, planInputs, stateOutputs, int(plan.Length.ValueInt64())` to `NextListOutputs` as the Task 4 body shows, then applies the same classification. The prior-data fill (`priorStamps`, `priorExpiresAfter`) from the Task 3/4 bodies stays as written.

### R.5 Update known path (both resources)

Replace the verbatim-copy branch with resolve-fresh-entries. Set version:

```go
	if !plan.Outputs.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// The plan's membership and keeper stamps reproduce exactly through
		// the same pure functions ModifyPlan used; only the fresh entries
		// (unknown in the plan) are resolved here with the apply clock
		// (spec section 6).
		planInputs, d := stringsFromList(ctx, plan.Inputs)
		resp.Diagnostics.Append(d...)
		planOutputs, d := stringsFromSet(ctx, plan.Outputs)
		resp.Diagnostics.Append(d...)
		priorStamps, d := stampsFromMap(ctx, state.DetailedOutputs)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		priorE, plannedE := int64Ptr(state.ExpiresAfter), int64Ptr(plan.ExpiresAfter)
		keep, fresh := accumulate.ClassifyStamps(planOutputs, planInputs, priorStamps, priorE, plannedE)
		stamps := make(map[string]time.Time, len(keep)+len(fresh))
		for v, stamp := range keep {
			stamps[v] = stamp
		}
		for v := range fresh {
			stamps[v] = accumulate.FreshStamp(priorStamps, priorE, plannedE, clockNow(), v)
		}
		detailed, d := mapFromStamps(ctx, planOutputs, stamps)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
```

The list version swaps the converters. The fallback path (whole plan unknown) is unchanged from the Task 3/4 bodies: `ApplyExpiration` with `clockNow()`.

### R.6 Create and ImportState

Unchanged from the Task 3/4 bodies. Create's known path copies the plan verbatim (on create every value comes from `inputs`, so `detailed_outputs` is all null and nothing is fresh); the fallback path never stamps for the same reason. ImportState seeding is unchanged.

### R.7 Schema description copy (both resources)

Replace the two attributes' `MarkdownDescription` final sentences. `expires_after` ends with:

```
... Expiration is realized at refresh: a value is removed when a refresh
runs after its `expires_at` has passed, and running with `-refresh=false`
defers removal until the next refresh. A late-applied saved plan will not
expire an item even if the time has passed by the time the plan is
applied — its refresh already ran at plan time; a new plan/apply is
required. The entry for a value that leaves `inputs` (or whose stamp is
being recalculated after an `expires_after` change) shows as known after
apply in that apply's plan. ...
```

`detailed_outputs` replaces its plan-time sentence with: "`expires_at` is an RFC 3339 timestamp of when the value will be removed, or null when the value cannot expire: it is in `inputs`, or `expires_after` is null. Entries being stamped by an apply show as known after apply in that apply's plan. Expiration is realized at refresh; a late-applied saved plan will not expire an item whose time passed after the plan was created — a new plan/apply is required."

Keep every real em dash; never `--` in these literals.

### R.8 Acceptance-test rework (Task 3 tests; Task 4 mirrors)

| Test | Change |
| --- | --- |
| `TestAccSetCreateWithExpiresAfter` | none (all-null entries are known) |
| `TestAccSetLeaverStampedOnInputsChange` | step 2 `PreApply`: replace the full-map `expectPlanDetailed` with `knownvalue.MapPartial` asserting only `a: null` (b's `expires_at` is unknown at plan). State checks unchanged. |
| `TestAccSetExpiryRealizedOnBareApply` → rename `TestAccSetExpiryRealizedOnRefresh` | step 3 (same config, `PreConfig` sleep): refresh culls b before planning, so assert `PreApply: ExpectEmptyPlan` and state `{a}` with `detailed {a: null}`; drop `ExpectNonEmptyPlan` |
| `TestAccSetInInputsNeverExpires` | none |
| `TestAccSetExpiresAfterSetOnExistingResource` | step 2 `PreApply`: `MapPartial` for `a: null` only (b fresh/unknown); state checks unchanged |
| `TestAccSetExpiresAfterRemoved` | none (stamp is far future) |
| `TestAccSetExpiresAfterZeroRemovesLeaverImmediately` → rename `TestAccSetExpiresAfterZeroCullsNextRefresh` | step 3 (E=0): b's entry is fresh (unknown at plan); apply stamps it "now"; state `{a, b}` with b stamped-shape. Add step 4 (same config): refresh culls b; empty plan; state `{a}` |
| `TestAccSetExpiresAfterDecreasePullsEarlier` | step 3 state: b stamped (min pulled to ~now+1s). Step 4 (same config, sleep): refresh culls b; `PreApply: ExpectEmptyPlan`; state `{a}`; drop `ExpectNonEmptyPlan` |
| `TestAccSetIncreaseRescuesExpiredUnrealized` → rename `TestAccSetIncreaseDoesNotResurrect` | step 3 (sleep, E 1→3600): refresh culls b before the plan can apply S4; assert plan and state `{a}` with `detailed {a: null}`; PostApplyPostRefresh empty |
| `TestAccSetReSupplyRescues` → semantics still hold | step 3 (sleep, inputs `[a, b]`, E=1): refresh culls b, the plan unions it back in (T5); assert state `{a, b}` all null; PostApplyPostRefresh empty |
| `TestAccSetResetClearsStamps`, `TestAccSetReplacementClearsStamps` | none (reseed is all-null) |
| `TestAccSetUnknownExpiresAfterAtPlan` | none (fallback path) |
| `TestAccSetImportThenExpiresAfter` | step 2 `PreApply`: `MapPartial` for `a: null` (b fresh); state checks unchanged |

For `MapPartial` assertions use `plancheck.ExpectKnownValue(addr, tfjsonpath.New("detailed_outputs"), knownvalue.MapPartial(map[string]knownvalue.Check{...}))`. The Task 4 list tests mirror this table; in particular `TestAccListExpiryRealizedOnBareApply` and `TestAccListDuplicatesExpireTogether` switch their final steps to refresh-culling assertions (`ExpectEmptyPlan` at `PreApply`, culled state) exactly as above.
