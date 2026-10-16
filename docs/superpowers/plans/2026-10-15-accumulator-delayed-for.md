# Accumulator delayed_for Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to work through this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `delayed_for` (optional warm-up window in seconds) to both accumulator resources, so a newly supplied value lands in a new computed `accumulated` attribute immediately but only appears in `outputs` after the window has passed.

**Architecture:** `accumulated` becomes the store that `NextListOutputs` / `NextSetOutputs` produce; `outputs` is `accumulated` filtered to entries whose `visible_at` (a second attribute inside each `detailed_outputs` entry) is null. New pure functions in `internal/accumulate/delay.go` classify entering and pending values without a clock; Read reveals due values with the refresh clock; Create and Update stamp fresh entries with the apply clock. A small `internal/provider/stamps.go` composes the expiry and delay classifications so both resources call one plan-side and one apply-side helper. `ValidateConfig` and ModifyPlan both reject `delayed_for >= expires_after`.

**Tech Stack:** unchanged: Go 1.26, `terraform-plugin-framework` v1.19.0, `terraform-plugin-framework-validators` v0.19.0, `terraform-plugin-testing` v1.16.0, tfplugindocs via the `tools/` module, OpenTofu for acceptance tests.

**Spec:** `docs/superpowers/specs/2026-10-15-accumulator-delayed-for-design.md` (approved). Read it alongside this plan; the plan argues from it. The expiry spec (`2026-09-23-accumulator-expiration-design.md`) still governs `expires_at`.

## Global Constraints

Every task's requirements implicitly include this section.

- **The plan never reads the clock.** ModifyPlan computes `outputs`, `accumulated`, and every keeper stamp as a pure function of prior state and configuration. Only `visible_at` / `expires_at` of entries this apply stamps are unknown in the plan. Read (refresh clock) and Create/Update (apply clock) are the only clock readers, always through `clockNow()`.
- **The plan shows `outputs` and `accumulated` fully known whenever it knows the branch inputs** (spec section 6; PR #2 invariant). The branch inputs are `inputs` (every element), `length`, both triggers, `expires_after`, and `delayed_for`, plus the mirrored state attributes. Every acceptance test that applies a known configuration asserts a known planned `outputs`.
- **The one invariant:** `outputs` equals `accumulated` filtered to null `visible_at`. Null `visible_at` means visible; non-null means pending.
- **`accumulated` is what the existing accumulation functions produce**, against the prior `accumulated` (or prior `outputs` when state predates the attribute). Trim first, then filter.
- **Reseed (create, replacement, `triggers_reset` change) means every value enters**: prior accumulated and prior stamps are both treated as empty for the delay classification (spec D1, T4).
- **Reveal boundary is inclusive:** `visible_at <= now` reveals (spec V1). Read culls expired values first, then reveals.
- **`delayed_for` is optional int64 seconds, `int64validator.Between(0, maxExpiresAfter)`**, no plan modifier. `delayed_for >= expires_after` with both known and non-null is an error on `delayed_for` (spec C1), raised by `ValidateConfig` and again by ModifyPlan.
- **`detailed_outputs` covers every value in `accumulated`**, one entry per distinct value, object `{expires_at, visible_at}`, both nullable strings, always concrete.
- **Import contract unchanged.** Imported state seeds `accumulated` equal to `outputs`, `delayed_for` null, every stamp null.
- **`internal/accumulate` stays Terraform-free.** `delay.go` imports only `time`; `boundary_test.go` enforces it.
- **License GPL-3.0-or-later.** Every new `.go` file starts with `// SPDX-License-Identifier: GPL-3.0-or-later`.
- **Em dashes in user-facing strings.** `TestUserFacingStringsUseEmDashes` fails any `--` inside a non-test string literal. Comments may use `--`.
- **House checks per task:** `gofmt -l .` empty, `go vet ./...` clean, `go test ./internal/...` green. Acceptance tests run with `make testacc TESTARGS='-run <Pattern>'`.
- **`make docs` generates the docs; never hand-edit them.** `docs/superpowers/` is hand-written. CI fails on a dirty tree after generation, so Task 5 runs `make docs` and commits the result.
- **Conventional Commits; stage explicit paths;** never `git add -A`. Work on branch `feat/delayed-for`.

## Review Focus

The five failure modes the spec implies that would bite a user first, each pinned by the named test in its owning task.

1. **A create or reseed with `delayed_for` must still plan `outputs` as known (empty) and `accumulated` as known (seeded).** If Create fell back to the unknown path, every downstream resource would plan as unknown. Pinned by `TestAccSetCreateWithDelayedFor` and `TestAccSetOutputsKnownAtPlanTimeWithDelayedFor` (Task 3), `TestAccListCreateWithDelayedFor` and `TestAccListOutputsKnownAtPlanTimeWithDelayedFor` (Task 4).
2. **Steady-state plan churn.** With pending values and no config change, the plan must be empty; a reveal must not plan an update. Pinned by the `PostApplyPostRefresh: ExpectEmptyPlan` on every 3600s step and by `TestAccSetRevealRealizedOnRefresh` / `TestAccListRevealRealizedOnRefresh` step 2 (`PreApply: ExpectEmptyPlan` after the reveal).
3. **Pre-feature state.** State written before this feature has `accumulated` null and entries without `visible_at`; Read must treat it as all-visible, fill `accumulated` from `outputs`, and leave the next plan empty. Pinned by `TestReadFillsNullAccumulated` (Task 3) and `TestStateGetToleratesMissingDelayAttributes` (Task 2).
4. **Reseed must restamp a value that was pending before the reset** with a full new window rather than carrying the old stamp. Pinned by the "reseed ignores prior stamps" case in `TestApplyDelay` (Task 1) and `TestAccSetResetDelaysSeed` (Task 3).
5. **Existing `expires_after` behavior must not change** when `delayed_for` is null: every pre-existing acceptance test must still pass with `visible_at` null in every entry. Pinned by the updated `nullDetail` / `stampedDetail` helpers (Task 2) and the unchanged expiry tests running green in Tasks 3 and 4.

Also unit-pinned: the inclusive reveal boundary and min/max direction on `delayed_for` changes (Task 1 tables).

## File Structure

| File | Responsibility | Task |
| --- | --- | --- |
| `internal/accumulate/delay.go` | `FilterPending`, `ClassifyDelays`, `Visible`, `ApplyDelay` | 1 |
| `internal/accumulate/delay_test.go` | Table-driven unit tests | 1 |
| `internal/provider/model.go` | `detailedObjectType` and `detailAttr` gain `visible_at`; `stamps` struct; `stampsFromMap`, `mapFromStamps`, `classifiedMap` take and return both stamp kinds | 2 |
| `internal/provider/stamps.go` | `stampArgs`, `planStamps`, `applyStamps`, `applyFallback`, `checkDelayShorterThanExpiry` | 2 |
| `internal/provider/model_test.go`, `internal/provider/stamps_test.go` | Unit tests for the above; state-shape tolerance | 2 |
| `internal/provider/read_test.go` | `TestReadFillsNullAccumulated`: Read over pre-feature state | 3 |
| `internal/provider/provider_test.go` | Schema test extended; shared acceptance helpers updated and added | 2 |
| `internal/provider/resource_set.go`, `resource_set_test.go` | `accumulator_set` wiring and acceptance tests | 3 |
| `internal/provider/resource_list.go`, `resource_list_test.go`, `import_test.go` | `accumulator_list` wiring and acceptance tests | 4 |
| `examples/resources/*/resource.tf`, `README.md`, `templates/index.md.tmpl`, `docs/**` | Examples and regenerated docs | 5 |

---

### Task 1: `accumulate` delay functions

**Files:**
- Create: `internal/accumulate/delay.go`
- Test: `internal/accumulate/delay_test.go`

**Interfaces:**
- Consumes: `FreshStamp(prior map[string]time.Time, priorExpiresAfter, plannedExpiresAfter *int64, now time.Time, value string) time.Time` from `expire.go` (the min/max arithmetic is identical for delay).
- Produces:
  - `func FilterPending(values []string, stamps map[string]time.Time, now time.Time) (visible []string, pending map[string]time.Time)`
  - `func ClassifyDelays(next, prior []string, priorStamps map[string]time.Time, priorDelayedFor, plannedDelayedFor *int64) (keep map[string]time.Time, fresh map[string]struct{})`
  - `func Visible(next []string, keep map[string]time.Time, fresh map[string]struct{}) []string`
  - `func ApplyDelay(next, prior []string, priorStamps map[string]time.Time, priorDelayedFor, plannedDelayedFor *int64, now time.Time) (visible []string, stamps map[string]time.Time)`
  - All slice and map returns are non-nil.

- [ ] **Step 1: Write the failing tests**

Create `internal/accumulate/delay_test.go` (`seconds` and `stampBase` already exist in `expire_test.go`, same package):

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"testing"
	"time"
)

// TestFilterPending pins the reveal rule V1 as Read applies it: a stamp not
// strictly after now is due, so the value is visible and its stamp dropped;
// a future stamp keeps the value pending.
func TestFilterPending(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values      []string
		stamps      map[string]time.Time
		now         time.Time
		wantVisible []string
		wantPending map[string]time.Time
	}{
		"stamp equal to now is revealed (V1 inclusive)": {
			values:      []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase},
			now:         stampBase,
			wantVisible: []string{"a"},
			wantPending: map[string]time.Time{},
		},
		"stamp strictly after now stays pending": {
			values:      []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Second)},
			now:         stampBase,
			wantVisible: []string{},
			wantPending: map[string]time.Time{"a": stampBase.Add(time.Second)},
		},
		"unstamped values are visible": {
			values:      []string{"a", "b"},
			stamps:      map[string]time.Time{"b": stampBase.Add(time.Hour)},
			now:         stampBase,
			wantVisible: []string{"a"},
			wantPending: map[string]time.Time{"b": stampBase.Add(time.Hour)},
		},
		"nil stamps reveals everything": {
			values:      []string{"a", "b"},
			stamps:      nil,
			now:         stampBase,
			wantVisible: []string{"a", "b"},
			wantPending: map[string]time.Time{},
		},
		"empty input yields empty output": {
			values:      []string{},
			stamps:      map[string]time.Time{},
			now:         stampBase,
			wantVisible: []string{},
			wantPending: map[string]time.Time{},
		},
		"order is preserved and duplicates share one decision": {
			values:      []string{"a", "b", "a", "c"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour), "c": stampBase.Add(-time.Second)},
			now:         stampBase,
			wantVisible: []string{"b", "c"},
			wantPending: map[string]time.Time{"a": stampBase.Add(time.Hour)},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			visible, pending := FilterPending(tc.values, tc.stamps, tc.now)
			if !reflect.DeepEqual(visible, tc.wantVisible) {
				t.Fatalf("visible = %v, want %v", visible, tc.wantVisible)
			}
			if visible == nil {
				t.Fatal("visible is nil; it must always be non-nil")
			}
			if !reflect.DeepEqual(pending, tc.wantPending) {
				t.Fatalf("pending = %v, want %v", pending, tc.wantPending)
			}
			if pending == nil {
				t.Fatal("pending is nil; it must always be non-nil")
			}
		})
	}
}

// TestClassifyDelays pins the clock-free split ModifyPlan uses (spec D1 to
// D5, T2): entering values are fresh, pending values under an unchanged
// delay are keepers, pending values under a changed delay are fresh, and
// visible values or anything under a null planned delay are in neither.
func TestClassifyDelays(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		next      []string
		prior     []string
		stamps    map[string]time.Time
		priorD    *int64
		plannedD  *int64
		wantKeep  map[string]time.Time
		wantFresh map[string]struct{}
	}{
		"entering value is fresh (D1)": {
			next:      []string{"a", "b"},
			prior:     []string{"a"},
			stamps:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"b": {}},
		},
		"pending value under an unchanged delay is kept (D2)": {
			next:      []string{"a"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:    seconds(300),
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{"a": stampBase.Add(time.Hour)},
			wantFresh: map[string]struct{}{},
		},
		"pending value under a decreased delay is fresh (D3)": {
			next:      []string{"a"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:    seconds(3600),
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}},
		},
		"pending value under an increased delay is fresh (D4)": {
			next:      []string{"a"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Minute)},
			priorD:    seconds(60),
			plannedD:  seconds(3600),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}},
		},
		"visible prior value is in neither map (D5)": {
			next:      []string{"a", "b"},
			prior:     []string{"a", "b"},
			stamps:    map[string]time.Time{},
			priorD:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{},
		},
		"null planned delay reveals everything (T2)": {
			next:      []string{"a", "b"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:    seconds(300),
			plannedD:  nil,
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{},
		},
		"nil prior means everything enters (create and reseed, T4)": {
			next:      []string{"a", "b"},
			prior:     nil,
			stamps:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}, "b": {}},
		},
		"duplicates in next classify once (list shape)": {
			next:      []string{"a", "a"},
			prior:     nil,
			stamps:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			keep, fresh := ClassifyDelays(tc.next, tc.prior, tc.stamps, tc.priorD, tc.plannedD)
			if !reflect.DeepEqual(keep, tc.wantKeep) {
				t.Fatalf("keep = %v, want %v", keep, tc.wantKeep)
			}
			if !reflect.DeepEqual(fresh, tc.wantFresh) {
				t.Fatalf("fresh = %v, want %v", fresh, tc.wantFresh)
			}
			if keep == nil || fresh == nil {
				t.Fatal("keep and fresh must always be non-nil")
			}
		})
	}
}

// TestVisible pins the invariant as a function: next minus every value in
// keep or fresh, order preserved, never nil.
func TestVisible(t *testing.T) {
	t.Parallel()

	got := Visible([]string{"a", "b", "c", "b"},
		map[string]time.Time{"b": stampBase},
		map[string]struct{}{"c": {}})
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Visible = %v, want %v", got, want)
	}
	if got := Visible(nil, nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("Visible(nil) = %#v, want empty non-nil", got)
	}
	if got := Visible([]string{"a", "b"}, nil, nil); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Visible with nothing hidden = %v, want [a b]", got)
	}
}

// TestApplyDelay pins the fallback composition: classify, stamp fresh with
// the apply clock, hide every stamped value. It never reveals; that is
// refresh's job (spec V1), so a zero delay leaves the value hidden here.
func TestApplyDelay(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		next        []string
		prior       []string
		stamps      map[string]time.Time
		priorD      *int64
		plannedD    *int64
		wantVisible []string
		wantStamps  map[string]time.Time
	}{
		"entering value is stamped now+D and hidden (D1)": {
			next:        []string{"a", "b"},
			prior:       []string{"a"},
			stamps:      nil,
			plannedD:    seconds(300),
			wantVisible: []string{"a"},
			wantStamps:  map[string]time.Time{"b": stampBase.Add(5 * time.Minute)},
		},
		"D removed reveals everything and stamps nothing (T2)": {
			next:        []string{"a", "b"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:      seconds(300),
			plannedD:    nil,
			wantVisible: []string{"a", "b"},
			wantStamps:  map[string]time.Time{},
		},
		"D of zero stamps now and still hides (V1 is refresh's job)": {
			next:        []string{"a"},
			prior:       nil,
			stamps:      nil,
			plannedD:    seconds(0),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase},
		},
		"unchanged D keeps the prior stamp (D2)": {
			next:        []string{"a"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:      seconds(3600),
			plannedD:    seconds(3600),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(time.Hour)},
		},
		"decrease keeps min(old, candidate) (D3)": {
			next:        []string{"a"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:      seconds(3600),
			plannedD:    seconds(300),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(5 * time.Minute)},
		},
		"increase keeps max(old, candidate) (D4)": {
			next:        []string{"a"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Minute)},
			priorD:      seconds(60),
			plannedD:    seconds(3600),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(time.Hour)},
		},
		"reseed ignores prior stamps: nil prior restamps a full window (T4)": {
			// The caller passes nil prior and nil stamps on reseed; a value
			// that was pending before the reset gets now+D, not its old stamp.
			next:        []string{"a"},
			prior:       nil,
			stamps:      nil,
			priorD:      seconds(300),
			plannedD:    seconds(300),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(5 * time.Minute)},
		},
		"visible prior value stays visible when D is set later (D5)": {
			next:        []string{"a", "b"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{},
			priorD:      nil,
			plannedD:    seconds(300),
			wantVisible: []string{"a"},
			wantStamps:  map[string]time.Time{"b": stampBase.Add(5 * time.Minute)},
		},
		"empty next yields empty results": {
			next:        []string{},
			prior:       nil,
			plannedD:    seconds(300),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			visible, stamps := ApplyDelay(tc.next, tc.prior, tc.stamps, tc.priorD, tc.plannedD, stampBase)
			if !reflect.DeepEqual(visible, tc.wantVisible) {
				t.Fatalf("visible = %v, want %v", visible, tc.wantVisible)
			}
			if !reflect.DeepEqual(stamps, tc.wantStamps) {
				t.Fatalf("stamps = %v, want %v", stamps, tc.wantStamps)
			}
			if visible == nil || stamps == nil {
				t.Fatal("visible and stamps must always be non-nil")
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/accumulate/ -run 'TestFilterPending|TestClassifyDelays|TestVisible|TestApplyDelay'`
Expected: build failure, `undefined: FilterPending` (and the other three names).

- [ ] **Step 3: Write `delay.go`**

Create `internal/accumulate/delay.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import "time"

// FilterPending partitions values by their stored visible_at stamps (spec
// V1, the reveal rule). A value with no stamp, or a stamp not strictly
// after now, is visible; the rest are pending. Order is preserved. pending
// holds only the stamps that are still in the future, so the caller writes
// every revealed entry back as null. Read calls it with the refresh clock.
// Both returns are non-nil.
func FilterPending(values []string, stamps map[string]time.Time, now time.Time) (visible []string, pending map[string]time.Time) {
	visible = make([]string, 0, len(values))
	pending = make(map[string]time.Time, len(stamps))
	for _, v := range values {
		stamp, ok := stamps[v]
		if ok && stamp.After(now) {
			pending[v] = stamp
			continue
		}
		visible = append(visible, v)
	}
	return visible, pending
}

// ClassifyDelays decides each value of next without a clock (spec D1 to
// D5, T2). keep holds prior pending stamps that survive unchanged: the
// value was in prior, has a stamp, and plannedDelayedFor equals
// priorDelayedFor (D2). fresh holds every value the apply must stamp: a
// value not in prior (D1, entering), or a pending value whose delay changed
// (D3, D4). A value in prior with no stamp is visible and appears in
// neither map (D5). When plannedDelayedFor is nil nothing is pending (T2).
// Callers pass nil prior and nil priorStamps on a reseed so every value
// enters with a full window (T4). Both returns are non-nil.
func ClassifyDelays(next, prior []string,
	priorStamps map[string]time.Time,
	priorDelayedFor, plannedDelayedFor *int64,
) (keep map[string]time.Time, fresh map[string]struct{}) {
	keep = make(map[string]time.Time)
	fresh = make(map[string]struct{})
	if plannedDelayedFor == nil {
		return keep, fresh
	}
	present := make(map[string]struct{}, len(prior))
	for _, v := range prior {
		present[v] = struct{}{}
	}
	unchanged := priorDelayedFor != nil && *priorDelayedFor == *plannedDelayedFor
	for _, v := range next {
		if _, wasPresent := present[v]; !wasPresent {
			fresh[v] = struct{}{}
			continue
		}
		stamp, pending := priorStamps[v]
		switch {
		case !pending:
			// Already visible: setting or changing delayed_for never hides it.
		case unchanged:
			keep[v] = stamp
		default:
			fresh[v] = struct{}{}
		}
	}
	return keep, fresh
}

// Visible returns next minus every value present in keep or fresh,
// preserving order. It is the invariant of spec section 3 as a function:
// outputs is accumulated minus the pending values. The result is non-nil.
func Visible(next []string, keep map[string]time.Time, fresh map[string]struct{}) []string {
	visible := make([]string, 0, len(next))
	for _, v := range next {
		if _, ok := keep[v]; ok {
			continue
		}
		if _, ok := fresh[v]; ok {
			continue
		}
		visible = append(visible, v)
	}
	return visible
}

// ApplyDelay is the fallback-path composition: classify, stamp fresh
// entries with the apply clock, and hide every stamped value. It never
// reveals: a stamp of exactly now still hides the value until the next
// refresh (spec V1), which is also what makes delayed_for = 0 mean "visible
// after the next refresh". now must be truncated to whole seconds
// (clockNow in internal/provider). Both returns are non-nil; stamps holds
// every pending value.
func ApplyDelay(next, prior []string,
	priorStamps map[string]time.Time,
	priorDelayedFor, plannedDelayedFor *int64,
	now time.Time) (visible []string, stamps map[string]time.Time) {

	keep, fresh := ClassifyDelays(next, prior, priorStamps, priorDelayedFor, plannedDelayedFor)
	stamps = make(map[string]time.Time, len(keep)+len(fresh))
	for v, stamp := range keep {
		stamps[v] = stamp
	}
	for v := range fresh {
		stamps[v] = FreshStamp(priorStamps, priorDelayedFor, plannedDelayedFor, now, v)
	}
	return Visible(next, keep, fresh), stamps
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/accumulate/`
Expected: PASS, including the existing `boundary_test.go` import guard.

- [ ] **Step 5: House checks and commit**

```bash
gofmt -l . && go vet ./... && go test ./internal/...
git add internal/accumulate/delay.go internal/accumulate/delay_test.go
git commit -m "feat: add delay classification to the accumulate package"
```

---

### Task 2: provider stamp helpers, shared test fixtures, and the configuration check

The provider boundary learns the second stamp, and the two compositions both resources will call land in one file so Tasks 3 and 4 are wiring only. This task also updates the shared acceptance helpers (`nullDetail`, `stampedDetail`, the schema test) so that every existing expiry acceptance test keeps passing once `visible_at` exists; nothing in `resource_*.go` changes yet, so `go test ./internal/...` stays green at the end of this task.

**Files:**
- Edit: `internal/provider/model.go` (`detailedObjectType`, `detailAttr`, `stampsFromMap`, `mapFromStamps`, `classifiedMap`)
- Create: `internal/provider/stamps.go`
- Edit: `internal/provider/model_test.go` (`TestMapFromStampsAndBack`, `TestClassifiedMap`, `TestStampsFromMapRejectsGarbageTimestamp`, `TestStateGetToleratesMissingExpirationAttributes`)
- Create: `internal/provider/stamps_test.go`
- Edit: `internal/provider/provider_test.go` (`TestProviderSchema`, `nullDetail`, `stampedDetail`, new helpers)

**Interfaces:**
- Consumes: Task 1's four functions; existing `FilterExpired`, `ClassifyStamps`, `FreshStamp`, `ApplyExpiration`.
- Produces (package `provider`):
  - `type stamps struct { Expires, Visible map[string]time.Time }`
  - `func stampsFromMap(ctx context.Context, m types.Map) (stamps, diag.Diagnostics)` — both maps non-nil; a missing or null attribute is "no stamp".
  - `func mapFromStamps(ctx context.Context, values []string, s stamps) (types.Map, diag.Diagnostics)`
  - `type classified struct { KeepExpires, KeepVisible map[string]time.Time; FreshExpires, FreshVisible map[string]struct{} }`
  - `func classifiedMap(ctx context.Context, values []string, c classified) (types.Map, diag.Diagnostics)`
  - `type stampArgs struct { PlanInputs, PriorAccumulated []string; Prior stamps; PriorExpires, PlannedExpires, PriorDelay, PlannedDelay *int64 }`
  - `func (a stampArgs) classify(accumulated []string) classified`
  - `func planStamps(ctx context.Context, accumulated []string, a stampArgs) (outputs []string, detailed types.Map, diags diag.Diagnostics)` — clock-free.
  - `func applyStamps(ctx context.Context, accumulated []string, a stampArgs, now time.Time) (types.Map, diag.Diagnostics)` — known-path apply.
  - `func applyFallback(ctx context.Context, merged []string, a stampArgs, now time.Time) (accumulated, outputs []string, detailed types.Map, diags diag.Diagnostics)` — unknown-path apply.
  - `func checkDelayShorterThanExpiry(delay, expires types.Int64) diag.Diagnostics`
  - Test helpers (package `provider_test`): `nullDetail()`, `stampedDetail()` (expires set, visible null), `pendingDetail()` (expires null, visible set), `pendingLeaverDetail()` (both set), `expectPlanDetailVisibleUnknown(addr, value)`, `expectAccumulated(addr string, check knownvalue.Check)`, `expectPlanAccumulated(addr string, check knownvalue.Check)`.

- [ ] **Step 1: Update the model tests and write the stamps tests**

In `internal/provider/model_test.go`, replace `TestMapFromStampsAndBack`, `TestClassifiedMap`, and `TestStampsFromMapRejectsGarbageTimestamp` with:

```go
func TestMapFromStampsAndBack(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	values := []string{"a", "b", "c", "d"}
	in := stamps{
		Expires: map[string]time.Time{"b": base.Add(5 * time.Minute), "d": base.Add(time.Hour)},
		Visible: map[string]time.Time{"c": base.Add(time.Minute), "d": base.Add(30 * time.Second)},
	}
	m, diags := mapFromStamps(ctx, values, in)
	if diags.HasError() {
		t.Fatalf("mapFromStamps: %+v", diags)
	}
	if m.IsNull() || m.IsUnknown() {
		t.Fatalf("mapFromStamps returned %v; it must be concrete", m)
	}
	if got := len(m.Elements()); got != 4 {
		t.Fatalf("map has %d entries, want 4", got)
	}

	parsed, diags := stampsFromMap(ctx, m)
	if diags.HasError() {
		t.Fatalf("stampsFromMap: %+v", diags)
	}
	if !reflect.DeepEqual(parsed, in) {
		t.Fatalf("round trip = %+v, want %+v", parsed, in)
	}
	// The all-null entry ("a") must not appear in either map: absence is
	// the null stamp.
	if _, ok := parsed.Expires["a"]; ok {
		t.Fatal("null expires_at round-tripped as a stamp")
	}
	if _, ok := parsed.Visible["a"]; ok {
		t.Fatal("null visible_at round-tripped as a stamp")
	}
}

// TestClassifiedMap pins the plan-shape split for both stamp kinds: keepers
// carry the prior stamp, fresh entries are unknown (the apply stamps them),
// and everything else is null. The map itself stays known.
func TestClassifiedMap(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	m, diags := classifiedMap(ctx, []string{"a", "b", "c", "d"}, classified{
		KeepExpires:  map[string]time.Time{"b": base.Add(5 * time.Minute)},
		FreshExpires: map[string]struct{}{"c": {}},
		KeepVisible:  map[string]time.Time{"c": base.Add(time.Minute)},
		FreshVisible: map[string]struct{}{"d": {}},
	})
	if diags.HasError() {
		t.Fatalf("classifiedMap: %+v", diags)
	}
	if m.IsNull() || m.IsUnknown() {
		t.Fatalf("classifiedMap returned %v; it must be a concrete map", m)
	}

	elems := m.Elements()
	if got := len(elems); got != 4 {
		t.Fatalf("map has %d entries, want 4", got)
	}
	attrOf := func(v, name string) types.String {
		t.Helper()
		obj, ok := elems[v].(types.Object)
		if !ok {
			t.Fatalf("entry %q is %T, want types.Object", v, elems[v])
		}
		s, ok := obj.Attributes()[name].(types.String)
		if !ok {
			t.Fatalf("entry %q %s is %T, want types.String", v, name, obj.Attributes()[name])
		}
		return s
	}

	if s := attrOf("a", "expires_at"); !s.IsNull() {
		t.Errorf("a expires_at = %v, want null", s)
	}
	if s := attrOf("a", "visible_at"); !s.IsNull() {
		t.Errorf("a visible_at = %v, want null", s)
	}
	if s := attrOf("b", "expires_at"); s.ValueString() != base.Add(5*time.Minute).Format(time.RFC3339) {
		t.Errorf("b expires_at = %q, want the kept stamp", s.ValueString())
	}
	if s := attrOf("c", "expires_at"); !s.IsUnknown() {
		t.Errorf("c expires_at = %v, want unknown", s)
	}
	if s := attrOf("c", "visible_at"); s.ValueString() != base.Add(time.Minute).Format(time.RFC3339) {
		t.Errorf("c visible_at = %q, want the kept stamp", s.ValueString())
	}
	if s := attrOf("d", "visible_at"); !s.IsUnknown() {
		t.Errorf("d visible_at = %v, want unknown", s)
	}
	if s := attrOf("d", "expires_at"); !s.IsNull() {
		t.Errorf("d expires_at = %v, want null", s)
	}
}

func TestStampsFromMapRejectsGarbageTimestamp(t *testing.T) {
	for _, name := range []string{"expires_at", "visible_at"} {
		attrs := map[string]attr.Value{
			"expires_at": types.StringNull(),
			"visible_at": types.StringNull(),
		}
		attrs[name] = types.StringValue("not-a-timestamp")
		m := types.MapValueMust(detailedObjectType, map[string]attr.Value{
			"b": types.ObjectValueMust(detailedObjectType.AttrTypes, attrs),
		})
		if _, diags := stampsFromMap(context.Background(), m); !diags.HasError() {
			t.Fatalf("stampsFromMap accepted a malformed %s; it must diagnose", name)
		}
	}
}

// TestStampsFromMapToleratesMissingVisibleAt pins spec section 9: an entry
// written before visible_at existed has no such attribute at all. The
// framework fills a missing object attribute with null on decode, but the
// accessor must also tolerate a raw object that lacks the key.
func TestStampsFromMapToleratesMissingVisibleAt(t *testing.T) {
	oldType := types.ObjectType{AttrTypes: map[string]attr.Type{"expires_at": types.StringType}}
	m := types.MapValueMust(oldType, map[string]attr.Value{
		"b": types.ObjectValueMust(oldType.AttrTypes, map[string]attr.Value{
			"expires_at": types.StringValue("2026-09-23T10:05:00Z"),
		}),
	})
	got, diags := stampsFromMap(context.Background(), m)
	if diags.HasError() {
		t.Fatalf("stampsFromMap on a pre-delay entry: %+v", diags)
	}
	if len(got.Visible) != 0 {
		t.Fatalf("Visible = %v, want empty", got.Visible)
	}
	if _, ok := got.Expires["b"]; !ok {
		t.Fatal("expires_at of the pre-delay entry was lost")
	}
}
```

Rename `TestStateGetToleratesMissingExpirationAttributes` to `TestStateGetToleratesMissingDelayAttributes` and extend its body: after the two existing `GetAttribute` calls add

```go
			var delayedFor types.Int64
			if diags := st.GetAttribute(ctx, path.Root("delayed_for"), &delayedFor); diags.HasError() {
				t.Fatalf("GetAttribute(delayed_for) on pre-delay state shape: %+v", diags)
			}
			if !delayedFor.IsNull() {
				t.Errorf("DelayedFor = %v, want null", delayedFor)
			}
```

and, for the `accumulated` attribute, a `types.Set` read for the set case and a `types.List` read for the list case, each asserting `IsNull()`. Add a `accumulatedType tftypes.Type` field to the table (`tftypes.Set{ElementType: stringType}` / `tftypes.List{ElementType: stringType}`) and decode into `attr.Value` via `st.GetAttribute(ctx, path.Root("accumulated"), &accumulated)` where `accumulated` is `types.Set` or `types.List` chosen by a `switch label`.

Create `internal/provider/stamps_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func seconds(n int64) *int64 { return &n }

var stampBase = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

// TestPlanStamps pins the clock-free plan composition: outputs is the
// visible subset, keepers carry known stamps, fresh entries are unknown.
func TestPlanStamps(t *testing.T) {
	ctx := context.Background()
	args := stampArgs{
		PlanInputs:       []string{"a", "c"},
		PriorAccumulated: []string{"a", "b"},
		Prior: stamps{
			Expires: map[string]time.Time{},
			Visible: map[string]time.Time{"b": stampBase.Add(time.Hour)},
		},
		PriorExpires:  seconds(7200),
		PlannedExpires: seconds(7200),
		PriorDelay:    seconds(3600),
		PlannedDelay:  seconds(3600),
	}
	outputs, detailed, diags := planStamps(ctx, []string{"a", "b", "c"}, args)
	if diags.HasError() {
		t.Fatalf("planStamps: %+v", diags)
	}
	// b is pending (kept), c is entering (fresh): both hidden.
	if want := []string{"a"}; !reflect.DeepEqual(outputs, want) {
		t.Fatalf("outputs = %v, want %v", outputs, want)
	}
	elems := detailed.Elements()
	b := elems["b"].(types.Object).Attributes()
	if s := b["visible_at"].(types.String); s.ValueString() != stampBase.Add(time.Hour).Format(time.RFC3339) {
		t.Errorf("b visible_at = %v, want the kept stamp", s)
	}
	// b left inputs with no prior expires stamp: fresh expires_at (S1).
	if s := b["expires_at"].(types.String); !s.IsUnknown() {
		t.Errorf("b expires_at = %v, want unknown", s)
	}
	c := elems["c"].(types.Object).Attributes()
	if s := c["visible_at"].(types.String); !s.IsUnknown() {
		t.Errorf("c visible_at = %v, want unknown", s)
	}
	if s := c["expires_at"].(types.String); !s.IsNull() {
		t.Errorf("c expires_at = %v, want null (in inputs)", s)
	}
}

// TestApplyStampsMatchesPlan pins that the apply resolves exactly the
// entries the plan left unknown and keeps every known entry bit for bit.
func TestApplyStampsMatchesPlan(t *testing.T) {
	ctx := context.Background()
	args := stampArgs{
		PlanInputs:       []string{"a", "c"},
		PriorAccumulated: []string{"a", "b"},
		Prior: stamps{
			Expires: map[string]time.Time{},
			Visible: map[string]time.Time{"b": stampBase.Add(time.Hour)},
		},
		PlannedExpires: seconds(7200),
		PriorDelay:     seconds(3600),
		PlannedDelay:   seconds(3600),
	}
	detailed, diags := applyStamps(ctx, []string{"a", "b", "c"}, args, stampBase)
	if diags.HasError() {
		t.Fatalf("applyStamps: %+v", diags)
	}
	got, diags := stampsFromMap(ctx, detailed)
	if diags.HasError() {
		t.Fatalf("stampsFromMap: %+v", diags)
	}
	want := stamps{
		Expires: map[string]time.Time{"b": stampBase.Add(2 * time.Hour)},
		Visible: map[string]time.Time{"b": stampBase.Add(time.Hour), "c": stampBase.Add(time.Hour)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stamps = %+v, want %+v", got, want)
	}
}

// TestApplyFallback pins the unknown-path composition: expiry culls, then
// delay hides, with one clock.
func TestApplyFallback(t *testing.T) {
	ctx := context.Background()
	args := stampArgs{
		PlanInputs:       []string{"a", "d"},
		PriorAccumulated: []string{"a", "b", "c"},
		Prior: stamps{
			Expires: map[string]time.Time{"b": stampBase.Add(-time.Second)},
			Visible: map[string]time.Time{"c": stampBase.Add(time.Hour)},
		},
		PriorExpires:   seconds(7200),
		PlannedExpires: seconds(7200),
		PriorDelay:     seconds(3600),
		PlannedDelay:   seconds(3600),
	}
	accumulated, outputs, detailed, diags := applyFallback(ctx, []string{"a", "b", "c", "d"}, args, stampBase)
	if diags.HasError() {
		t.Fatalf("applyFallback: %+v", diags)
	}
	if want := []string{"a", "c", "d"}; !reflect.DeepEqual(accumulated, want) {
		t.Fatalf("accumulated = %v, want %v (b expired)", accumulated, want)
	}
	if want := []string{"a"}; !reflect.DeepEqual(outputs, want) {
		t.Fatalf("outputs = %v, want %v (c pending, d entering)", outputs, want)
	}
	if got := len(detailed.Elements()); got != 3 {
		t.Fatalf("detailed has %d entries, want 3", got)
	}
}

func TestCheckDelayShorterThanExpiry(t *testing.T) {
	cases := map[string]struct {
		delay, expires types.Int64
		wantErr        bool
	}{
		"both null":          {types.Int64Null(), types.Int64Null(), false},
		"delay only":         {types.Int64Value(10), types.Int64Null(), false},
		"expires only":       {types.Int64Null(), types.Int64Value(10), false},
		"delay shorter":      {types.Int64Value(9), types.Int64Value(10), false},
		"delay equal":        {types.Int64Value(10), types.Int64Value(10), true},
		"delay longer":       {types.Int64Value(11), types.Int64Value(10), true},
		"delay unknown":      {types.Int64Unknown(), types.Int64Value(10), false},
		"expires unknown":    {types.Int64Value(10), types.Int64Unknown(), false},
		"both zero":          {types.Int64Value(0), types.Int64Value(0), true},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			diags := checkDelayShorterThanExpiry(tc.delay, tc.expires)
			if diags.HasError() != tc.wantErr {
				t.Fatalf("diags = %+v, wantErr %v", diags, tc.wantErr)
			}
		})
	}
}
```

In `internal/provider/provider_test.go`:

1. In `TestProviderSchema`, change the `detailed_outputs` type check to

```go
		case !detailed.Type.Is(tftypes.Map{ElementType: tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"expires_at": tftypes.String, "visible_at": tftypes.String}}}):
			t.Errorf("%s: detailed_outputs type = %s, want map(object({expires_at=string, visible_at=string}))", name, detailed.Type)
```

and add, after the `detailed` block:

```go
		delayedFor, ok := attrs["delayed_for"]
		switch {
		case !ok:
			t.Errorf("%s: missing delayed_for attribute", name)
		case !delayedFor.Optional || delayedFor.Required || delayedFor.Computed:
			t.Errorf("%s: delayed_for must be Optional only, got %+v", name, delayedFor)
		case !delayedFor.Type.Is(tftypes.Number):
			t.Errorf("%s: delayed_for type = %s, want number", name, delayedFor.Type)
		}
		accumulated, ok := attrs["accumulated"]
		wantAccumulated := tftypes.Type(tftypes.Set{ElementType: tftypes.String})
		if name == "accumulator_list" {
			wantAccumulated = tftypes.List{ElementType: tftypes.String}
		}
		switch {
		case !ok:
			t.Errorf("%s: missing accumulated attribute", name)
		case !accumulated.Computed || accumulated.Optional || accumulated.Required:
			t.Errorf("%s: accumulated must be Computed only, got %+v", name, accumulated)
		case !accumulated.Type.Is(wantAccumulated):
			t.Errorf("%s: accumulated type = %s, want %s", name, accumulated.Type, wantAccumulated)
		}
```

2. Replace `nullDetail` and `stampedDetail`, and add the new helpers:

```go
// rfc3339 matches an RFC 3339 UTC timestamp at second precision. The
// concrete instant comes from the apply clock, so acceptance tests match
// the shape, not the time.
var rfc3339 = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

func detail(expires, visible knownvalue.Check) knownvalue.Check {
	return knownvalue.ObjectExact(map[string]knownvalue.Check{
		"expires_at": expires,
		"visible_at": visible,
	})
}

// nullDetail is one detailed_outputs entry with both stamps null: the
// value is visible and cannot expire.
func nullDetail() knownvalue.Check { return detail(knownvalue.Null(), knownvalue.Null()) }

// stampedDetail is a visible value with an expires_at stamp.
func stampedDetail() knownvalue.Check {
	return detail(knownvalue.StringRegexp(rfc3339), knownvalue.Null())
}

// pendingDetail is a value hidden behind a visible_at stamp that cannot
// expire (it is in inputs).
func pendingDetail() knownvalue.Check {
	return detail(knownvalue.Null(), knownvalue.StringRegexp(rfc3339))
}

// pendingLeaverDetail is a value that is both pending and expiring (spec
// T5).
func pendingLeaverDetail() knownvalue.Check {
	return detail(knownvalue.StringRegexp(rfc3339), knownvalue.StringRegexp(rfc3339))
}

// expectPlanDetailVisibleUnknown checks one planned entry's visible_at is
// unknown: the value enters at this apply and the apply stamps it.
func expectPlanDetailVisibleUnknown(addr, value string) plancheck.PlanCheck {
	return plancheck.ExpectUnknownValue(addr,
		tfjsonpath.New("detailed_outputs").AtMapKey(value).AtMapKey("visible_at"))
}

// expectAccumulated checks the whole accumulated value in state.
func expectAccumulated(addr string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New("accumulated"), check)
}

// expectPlanAccumulated checks the planned accumulated is known and
// matches: the plan must show the store the apply will produce.
func expectPlanAccumulated(addr string, check knownvalue.Check) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue(addr, tfjsonpath.New("accumulated"), check)
}
```

Remove the now-unused `regexp` literal inside the old `stampedDetail`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run 'TestMapFromStampsAndBack|TestClassifiedMap|TestStampsFromMap|TestPlanStamps|TestApplyStamps|TestApplyFallback|TestCheckDelay|TestProviderSchema'`
Expected: build failure (`undefined: stamps`, `undefined: classified`, `undefined: stampArgs`, …).

- [ ] **Step 3: Update `model.go`**

Replace `detailedObjectType`, `detailAttr`, `stampsFromMap`, `mapFromStamps`, and `classifiedMap` in `internal/provider/model.go` with:

```go
// detailedObjectType is the element type of detailed_outputs on both
// resources: an object with two nullable RFC 3339 strings. expires_at is
// when the value leaves accumulated (null: cannot expire); visible_at is
// when a pending value joins outputs (null: already visible).
var detailedObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"expires_at": types.StringType,
		"visible_at": types.StringType,
	},
}

// detailAttr is the Go shape of one detailed_outputs entry. A null stamp
// is written as null, never as a zero time.
type detailAttr struct {
	ExpiresAt types.String `tfsdk:"expires_at"`
	VisibleAt types.String `tfsdk:"visible_at"`
}

// stamps holds both stamp kinds for a set of values, keyed by value.
// Absence from a map means the corresponding attribute is null.
type stamps struct {
	Expires map[string]time.Time
	Visible map[string]time.Time
}

// stampsFromMap reads detailed_outputs into the two value-to-stamp maps
// the pure functions take. A null or unknown map (state written before
// the attribute existed) yields empty maps; an entry whose attribute is
// null, unknown, or absent (state written before visible_at existed,
// spec section 9) contributes no stamp. Both maps are non-nil.
func stampsFromMap(ctx context.Context, m types.Map) (stamps, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := stamps{
		Expires: make(map[string]time.Time),
		Visible: make(map[string]time.Time),
	}
	if m.IsNull() || m.IsUnknown() {
		return out, diags
	}
	for key, elem := range m.Elements() {
		obj, ok := elem.(types.Object)
		if !ok {
			diags.AddError("Invalid detailed_outputs state",
				fmt.Sprintf("entry %q is not an object; the state file is corrupt", key))
			return out, diags
		}
		attrs := obj.Attributes()
		for name, target := range map[string]map[string]time.Time{
			"expires_at": out.Expires,
			"visible_at": out.Visible,
		} {
			raw, present := attrs[name]
			if !present {
				continue
			}
			s, ok := raw.(types.String)
			if !ok {
				diags.AddError("Invalid detailed_outputs state",
					fmt.Sprintf("entry %q has a non-string %s; the state file is corrupt", key, name))
				return out, diags
			}
			if s.IsNull() || s.IsUnknown() {
				continue
			}
			stamp, err := time.Parse(time.RFC3339, s.ValueString())
			if err != nil {
				diags.AddError(fmt.Sprintf("Invalid %s in state", name),
					fmt.Sprintf("entry %q %s %q is not an RFC 3339 timestamp: %v", key, name, s.ValueString(), err))
				return out, diags
			}
			target[key] = stamp
		}
	}
	return out, diags
}

// formatStamp renders a stamp RFC 3339 UTC, or null when absent.
func formatStamp(m map[string]time.Time, v string) types.String {
	if stamp, ok := m[v]; ok {
		return types.StringValue(stamp.UTC().Format(time.RFC3339))
	}
	return types.StringNull()
}

// mapFromStamps builds detailed_outputs from values and their stamps: one
// entry per distinct value, each attribute formatted RFC 3339 UTC or null.
// Duplicate values collapse into one entry, which is the documented
// list-resource shape. An empty values yields an empty map, never null.
func mapFromStamps(ctx context.Context, values []string, s stamps) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(values))
	for _, v := range values {
		entries[v] = detailAttr{
			ExpiresAt: formatStamp(s.Expires, v),
			VisibleAt: formatStamp(s.Visible, v),
		}
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}

// classified is the clock-free plan-time decision for every value in
// accumulated: which stamps survive as known values and which the apply
// must create. A value absent from all four is null on both attributes.
type classified struct {
	KeepExpires  map[string]time.Time
	KeepVisible  map[string]time.Time
	FreshExpires map[string]struct{}
	FreshVisible map[string]struct{}
}

// classifiedMap builds detailed_outputs for the plan: a null attribute for
// values with no stamp, the formatted prior stamp for keepers, and an
// unknown attribute for fresh entries the apply will stamp. The map keys
// stay known; only fresh attributes are unknown. It reads no clock.
func classifiedMap(ctx context.Context, values []string, c classified) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	entries := make(map[string]detailAttr, len(values))
	for _, v := range values {
		entry := detailAttr{
			ExpiresAt: formatStamp(c.KeepExpires, v),
			VisibleAt: formatStamp(c.KeepVisible, v),
		}
		if _, ok := c.FreshExpires[v]; ok {
			entry.ExpiresAt = types.StringUnknown()
		}
		if _, ok := c.FreshVisible[v]; ok {
			entry.VisibleAt = types.StringUnknown()
		}
		entries[v] = entry
	}
	m, more := types.MapValueFrom(ctx, detailedObjectType, entries)
	diags.Append(more...)
	return m, diags
}
```

Reword the doc comment of `maxExpiresAfter` to say "Both resources check `expires_after` and `delayed_for` against it."

- [ ] **Step 4: Write `stamps.go`**

Create `internal/provider/stamps.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

// stampArgs carries everything the stamp decisions need besides the
// accumulated result itself. On a create or reseed the caller leaves
// PriorAccumulated nil and Prior zero, so every value enters with a full
// delay window and no expiry stamp survives (spec T4).
type stampArgs struct {
	PlanInputs       []string
	PriorAccumulated []string
	Prior            stamps
	PriorExpires     *int64
	PlannedExpires   *int64
	PriorDelay       *int64
	PlannedDelay     *int64
}

// classify runs both clock-free classifications over accumulated.
func (a stampArgs) classify(accumulated []string) classified {
	var c classified
	c.KeepExpires, c.FreshExpires = accumulate.ClassifyStamps(accumulated, a.PlanInputs,
		a.Prior.Expires, a.PriorExpires, a.PlannedExpires)
	c.KeepVisible, c.FreshVisible = accumulate.ClassifyDelays(accumulated, a.PriorAccumulated,
		a.Prior.Visible, a.PriorDelay, a.PlannedDelay)
	return c
}

// planStamps is the known-path plan composition (spec section 6): outputs
// is accumulated minus pending values, and detailed_outputs carries known
// keeper stamps and unknown fresh ones. It reads no clock.
func planStamps(ctx context.Context, accumulated []string, a stampArgs) ([]string, types.Map, diag.Diagnostics) {
	c := a.classify(accumulated)
	outputs := accumulate.Visible(accumulated, c.KeepVisible, c.FreshVisible)
	detailed, diags := classifiedMap(ctx, accumulated, c)
	return outputs, detailed, diags
}

// applyStamps is the known-path apply composition: it reproduces the plan's
// classification through the same pure functions and resolves each fresh
// entry with the apply clock. Membership (outputs, accumulated) comes from
// the plan verbatim; only detailed_outputs is rebuilt here.
func applyStamps(ctx context.Context, accumulated []string, a stampArgs, now time.Time) (types.Map, diag.Diagnostics) {
	c := a.classify(accumulated)
	s := stamps{
		Expires: make(map[string]time.Time, len(c.KeepExpires)+len(c.FreshExpires)),
		Visible: make(map[string]time.Time, len(c.KeepVisible)+len(c.FreshVisible)),
	}
	for v, stamp := range c.KeepExpires {
		s.Expires[v] = stamp
	}
	for v := range c.FreshExpires {
		s.Expires[v] = accumulate.FreshStamp(a.Prior.Expires, a.PriorExpires, a.PlannedExpires, now, v)
	}
	for v, stamp := range c.KeepVisible {
		s.Visible[v] = stamp
	}
	for v := range c.FreshVisible {
		s.Visible[v] = accumulate.FreshStamp(a.Prior.Visible, a.PriorDelay, a.PlannedDelay, now, v)
	}
	return mapFromStamps(ctx, accumulated, s)
}

// applyFallback is the unknown-path apply composition: expiry culls merged
// with the apply clock, then the delay hides entering and pending values.
// It returns the new accumulated (survivors), outputs (visible survivors),
// and detailed_outputs.
func applyFallback(ctx context.Context, merged []string, a stampArgs, now time.Time) ([]string, []string, types.Map, diag.Diagnostics) {
	accumulated, expires := accumulate.ApplyExpiration(merged, a.PlanInputs,
		a.Prior.Expires, a.PriorExpires, a.PlannedExpires, now)
	outputs, visible := accumulate.ApplyDelay(accumulated, a.PriorAccumulated,
		a.Prior.Visible, a.PriorDelay, a.PlannedDelay, now)
	detailed, diags := mapFromStamps(ctx, accumulated, stamps{Expires: expires, Visible: visible})
	return accumulated, outputs, detailed, diags
}

// checkDelayShorterThanExpiry is spec C1: with both attributes known and
// non-null, delayed_for must be strictly less than expires_after, or a
// value could expire before it ever became visible. ValidateConfig runs it
// on configuration and ModifyPlan repeats it on the plan, so a pair that
// was unknown at validation still fails at plan rather than at apply.
func checkDelayShorterThanExpiry(delay, expires types.Int64) diag.Diagnostics {
	var diags diag.Diagnostics
	if delay.IsNull() || delay.IsUnknown() || expires.IsNull() || expires.IsUnknown() {
		return diags
	}
	if delay.ValueInt64() >= expires.ValueInt64() {
		diags.AddAttributeError(path.Root("delayed_for"),
			"delayed_for must be shorter than expires_after",
			fmt.Sprintf("delayed_for is %d seconds but expires_after is %d seconds; a value must "+
				"become visible before it can expire, so delayed_for must be strictly less than "+
				"expires_after.", delay.ValueInt64(), expires.ValueInt64()))
	}
	return diags
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/provider/ -run 'TestMapFromStampsAndBack|TestClassifiedMap|TestStampsFromMap|TestPlanStamps|TestApplyStamps|TestApplyFallback|TestCheckDelay|TestProviderSchema|TestStateGet'`
Expected: everything passes except `TestProviderSchema`, which fails on the missing `delayed_for` and `accumulated` attributes and the old `detailed_outputs` type until Tasks 3 and 4 add them. Also run `go build ./...`: `resource_set.go` and `resource_list.go` no longer compile against the new `stampsFromMap` / `mapFromStamps` / `classifiedMap` signatures. Make the minimal compile fix in both files now, so the tree builds at this commit: at every `stampsFromMap` call, take `.Expires` from the struct; at every `mapFromStamps(ctx, values, stampsMap)` call, pass `stamps{Expires: stampsMap}`; at every `classifiedMap(ctx, values, keep, fresh)` call, pass `classified{KeepExpires: keep, FreshExpires: fresh}`. Then `go test ./internal/...` is green except `TestProviderSchema`.

- [ ] **Step 6: House checks and commit**

```bash
gofmt -l . && go vet ./... && go test ./internal/...
git add internal/provider/model.go internal/provider/model_test.go internal/provider/stamps.go internal/provider/stamps_test.go internal/provider/provider_test.go internal/provider/resource_set.go internal/provider/resource_list.go
git commit -m "feat: add visible_at stamps and shared stamp compositions"
```

`TestProviderSchema` stays red at this commit by design; Task 3 turns half of it green and Task 4 the rest.

---

### Task 3: `accumulator_set` wiring and acceptance tests

**Files:**
- Edit: `internal/provider/resource_set.go`
- Create: `internal/provider/read_test.go`
- Test: `internal/provider/resource_set_test.go`

**Interfaces:**
- Consumes: Task 2's `stampArgs`, `planStamps`, `applyStamps`, `applyFallback`, `checkDelayShorterThanExpiry`, `stampsFromMap`, `mapFromStamps`; Task 1's `FilterPending`; existing `FilterExpired`, `NextSetOutputs`.
- Produces: `setResourceModel` with `DelayedFor types.Int64` and `Accumulated types.Set`; `(*setResource).ValidateConfig`.

- [ ] **Step 1: Write the failing acceptance tests**

Create `internal/provider/read_test.go` (package `provider`, so it can call `Read` directly):

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestReadFillsNullAccumulated pins spec section 9 for Read: state written
// before this feature arrives with accumulated null (the framework fills a
// missing attribute with null on decode). Read must write accumulated as a
// copy of outputs, so the next plan is empty, and leave everything else
// alone.
func TestReadFillsNullAccumulated(t *testing.T) {
	ctx := context.Background()
	sch := resourceSchema(t, &setResource{})
	stringType := tftypes.String
	detailType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"expires_at": stringType, "visible_at": stringType}}
	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"inputs":               tftypes.NewValue(tftypes.List{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
		"triggers_reset":       tftypes.NewValue(stringType, nil),
		"triggers_replacement": tftypes.NewValue(stringType, nil),
		"expires_after":        tftypes.NewValue(tftypes.Number, nil),
		"delayed_for":          tftypes.NewValue(tftypes.Number, nil),
		"accumulated":          tftypes.NewValue(tftypes.Set{ElementType: stringType}, nil),
		"outputs":              tftypes.NewValue(tftypes.Set{ElementType: stringType}, []tftypes.Value{tftypes.NewValue(stringType, "a")}),
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
	(&setResource{}).Read(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %+v", resp.Diagnostics)
	}
	var state setResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("State.Get: %+v", diags)
	}
	got, _ := stringsFromSet(ctx, state.Accumulated)
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("accumulated after Read = %v, want %v", got, want)
	}
	out, _ := stringsFromSet(ctx, state.Outputs)
	if !reflect.DeepEqual(out, []string{"a"}) {
		t.Fatalf("outputs after Read = %v, want [a]", out)
	}
}
```

Add to `internal/provider/resource_set_test.go`:

```go
func expectSetAccumulated(values ...string) statecheck.StateCheck {
	return expectAccumulated("accumulator_set.test", knownvalue.SetExact(stringList(values...)))
}

func expectSetPlanAccumulated(values ...string) plancheck.PlanCheck {
	return expectPlanAccumulated("accumulator_set.test", knownvalue.SetExact(stringList(values...)))
}

// TestAccSetCreateWithDelayedFor pins D1 on create: outputs plans as known
// and empty, accumulated as known and seeded, and every visible_at is
// unknown until the apply stamps it. The 3600s window keeps the
// post-refresh plan empty.
func TestAccSetCreateWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: setConfig(`["a", "b"]`, `  delayed_for = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectSetPlanOutputs(),
					expectSetPlanAccumulated("a", "b"),
					expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					expectPlanDetailVisibleUnknown("accumulator_set.test", "b"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs(),
				expectSetAccumulated("a", "b"),
				expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
					"a": pendingDetail(), "b": pendingDetail(),
				}),
			},
		}},
	})
}

// TestAccSetOutputsKnownAtPlanTimeWithDelayedFor extends the PR #2 suite:
// union, reset, and replacement all plan outputs and accumulated as known
// while delayed_for is set.
func TestAccSetOutputsKnownAtPlanTimeWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
			{
				// Union: a stays visible (D5), b enters and hides (D1).
				Config: setConfig(`["a", "b"]`, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a"),
						expectSetPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectSetAccumulated("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": pendingDetail(),
					}),
				},
			},
			{
				// Reset: everything reseeds and enters (T4).
				Config: setConfig(`["a", "b"]`, `  delayed_for = 3600
  triggers_reset = "v1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs(), expectSetAccumulated("a", "b")},
			},
			{
				// Replacement: Create reseeds and enters (T4).
				Config: setConfig(`["a", "b"]`, `  delayed_for = 3600
  triggers_reset = "v1"
  triggers_replacement = "r1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_set.test", plancheck.ResourceActionReplace),
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs(), expectSetAccumulated("a", "b")},
			},
		},
	})
}

// TestAccSetRevealRealizedOnRefresh pins V1: with no configuration change,
// the step's refresh reveals a before planning, so the plan is empty and
// outputs now holds it with a null visible_at.
func TestAccSetRevealRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a"]`, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", setDetail("a", pendingDetail())),
				},
			},
			{
				Config:    setConfig(`["a"]`, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a"),
					expectSetAccumulated("a"),
					expectDetailed("accumulator_set.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccSetDelayedForRemoved pins T2: removing delayed_for reveals every
// pending value at that apply, and the plan shows the revealed outputs.
func TestAccSetDelayedForRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a", "b"]`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						expectPlanDetailed("accumulator_set.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			},
		},
	})
}

// TestAccSetDelayedForDecreaseRevealsSooner pins D3 end to end: a was
// stamped +1h; decreasing to 1 recalculates min(old, now+1s) at apply, and
// the following refresh reveals it. Step 2 leaves a 1s stamp pending, so it
// carries no post-refresh check.
func TestAccSetDelayedForDecreaseRevealsSooner(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectDetailed("accumulator_set.test", setDetail("a", pendingDetail())),
				},
			},
			{
				Config:    setConfig(`["a"]`, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
		},
	})
}

// TestAccSetDelayedForIncreaseKeepsPending pins D4: an increase restamps
// the pending value (fresh, unknown in the plan) and it stays hidden.
func TestAccSetDelayedForIncreaseKeepsPending(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 7200`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", setDetail("a", pendingDetail())),
				},
			},
		},
	})
}

// TestAccSetDelayedForZeroRevealsNextRefresh pins the delayed_for = 0
// reading of V1: the apply stamps the current moment and leaves outputs
// unchanged; the next refresh reveals.
func TestAccSetDelayedForZeroRevealsNextRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: setConfig(`["a"]`, `  delayed_for = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{expectSetPlanOutputs()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// The apply stamps a with the current moment; the refresh
					// that reveals it runs after these checks.
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", setDetail("a", pendingDetail())),
				},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 0`),
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

// TestAccSetDelayedForMustBeShorterThanExpiresAfter pins C1: equal and
// greater are rejected before any plan; shorter is accepted.
func TestAccSetDelayedForMustBeShorterThanExpiresAfter(t *testing.T) {
	for _, tc := range []struct{ name, extra string }{
		{"equal", "  delayed_for = 10\n  expires_after = 10"},
		{"greater", "  delayed_for = 11\n  expires_after = 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      setConfig(`["a"]`, tc.extra),
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
				Config:            setConfig(`["a"]`, "  delayed_for = 9\n  expires_after = 10"),
				ConfigStateChecks: []statecheck.StateCheck{expectSetAccumulated("a")},
			}},
		})
	})
}

// TestAccSetPendingLeaverStamped pins T5: a value that leaves inputs while
// pending keeps visible_at and gains expires_at.
func TestAccSetPendingLeaverStamped(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a", "b"]`, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config: setConfig(`["a"]`, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a", "b"),
						expectPlanDetailUnknown("accumulator_set.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": pendingDetail(), "b": pendingLeaverDetail(),
					}),
				},
			},
		},
	})
}

// TestAccSetResetDelaysSeed pins T4 with a value that was already visible:
// a reset reseeds and the seeded value enters again with a full window.
func TestAccSetResetDelaysSeed(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs()},
			},
			{
				Config:            setConfig(`["a"]`, `  delayed_for = 1`),
				PreConfig:         sleepPastExpiry(),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
			},
			{
				Config: setConfig(`["a"]`, `  delayed_for = 3600
  triggers_reset = "v2"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs(),
						expectSetPlanAccumulated("a"),
						expectPlanDetailVisibleUnknown("accumulator_set.test", "a"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs(),
					expectDetailed("accumulator_set.test", setDetail("a", pendingDetail())),
				},
			},
		},
	})
}

// TestAccSetUnknownDelayedForAtPlan pins the fallback path: an unknown
// delayed_for leaves outputs, accumulated, and detailed_outputs unknown at
// plan, and the apply resolves them with its own clock.
func TestAccSetUnknownDelayedForAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_set" "test" {
  inputs      = ["a"]
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
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("accumulated")),
					plancheck.ExpectUnknownValue("accumulator_set.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs(),
				expectSetAccumulated("a"),
				expectDetailed("accumulator_set.test", setDetail("a", pendingDetail())),
			},
		}},
	})
}

// TestAccSetImportThenDelayedFor pins spec section 8: imported values seed
// visible, and the first new value in the settling apply is delayed.
func TestAccSetImportThenDelayedFor(t *testing.T) {
	config := `
resource "accumulator_set" "test" {
  inputs      = ["d"]
  delayed_for = 3600
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
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectSetPlanOutputs("a", "b"),
						expectSetPlanAccumulated("a", "b", "d"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b"),
					expectDetailed("accumulator_set.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(), "d": pendingDetail(),
					}),
				},
			},
		},
	})
}
```

Also update the two existing import tests' expectations where they assert `detailed_outputs` (none in `resource_set_test.go` need changing; `nullDetail` already carries `visible_at: null`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go vet ./internal/provider/`
Expected: vet passes (Task 2 added the helpers), but `make testacc TESTARGS='-run TestAccSetCreateWithDelayedFor'` fails with an unsupported argument `delayed_for`.

- [ ] **Step 3: Wire the resource**

In `internal/provider/resource_set.go`:

1. Add the interface assertion `_ resource.ResourceWithValidateConfig = (*setResource)(nil)`.

2. Add to `setResourceModel`, after `ExpiresAfter`:

```go
	DelayedFor          types.Int64  `tfsdk:"delayed_for"`
	Accumulated         types.Set    `tfsdk:"accumulated"`
```

3. Schema. Extend the resource description with: "With `delayed_for` set, a newly supplied value lands in `accumulated` at once but joins `outputs` only after the delay; see `delayed_for` for the clock rules." Add the two attributes and update `detailed_outputs` and `outputs`:

```go
			"delayed_for": schema.Int64Attribute{
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, maxExpiresAfter)},
				MarkdownDescription: "How many seconds a value waits in `accumulated` before it appears in " +
					"`outputs`. The apply that first accumulates a value stamps its `visible_at` as now " +
					"plus `delayed_for` and leaves it out of `outputs`; the reveal is realized at " +
					"refresh: a value joins `outputs` when a refresh runs after its `visible_at` has " +
					"passed, and running with `-refresh=false` defers the reveal until the next refresh. " +
					"A late-applied saved plan will not reveal an item even if the time has passed by " +
					"the time the plan is applied — its refresh already ran at plan time; a new " +
					"plan/apply is required. Create, replacement, and a `triggers_reset` change delay " +
					"every seeded value. Setting `delayed_for` on an existing resource hides nothing " +
					"that is already visible. Changing the value recalculates every pending " +
					"`visible_at` as min(old, new) on a decrease and max(old, new) on an increase; " +
					"removing it reveals every pending value at that apply. Must be between 0 and " +
					"9223372036 (about 292 years) and strictly less than `expires_after` when both are " +
					"set; `0` stamps the current moment so the next refresh reveals the value. Leave it " +
					"null to disable the delay entirely.",
			},
			"accumulated": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Every value ever accumulated, each appearing once, including " +
					"values still waiting out `delayed_for`. `outputs` is this set minus the pending " +
					"values; with `delayed_for` null the two are always equal. Computed; never " +
					"configured. Known at plan time under the same conditions as `outputs`.",
			},
```

`detailed_outputs` description becomes:

```text
A map from every value in `accumulated` to its timing attributes. `expires_at` is an RFC 3339 timestamp of when the value will be removed, or null when the value cannot expire: it is in `inputs`, or `expires_after` is null. `visible_at` is an RFC 3339 timestamp of when a pending value will join `outputs`, or null when it is already visible. Computed; never configured. Entries being stamped by an apply show as known after apply in that apply's plan. Expiry and reveal are realized at refresh; a late-applied saved plan will not expire or reveal an item whose time passed after the plan was created — a new plan/apply is required.
```

`outputs` description: replace "Every value ever accumulated, each appearing once." with "The visible values: `accumulated` minus every value still waiting out `delayed_for`, each appearing once." and add `delayed_for` to the list of attributes whose unknown-ness makes `outputs` unknown.

4. Add `ValidateConfig`:

```go
// ValidateConfig is spec C1 on raw configuration: delayed_for must be
// strictly less than expires_after when both are known. Unknown values
// skip the check here; ModifyPlan repeats it once they resolve.
func (r *setResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config setResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(config.DelayedFor, config.ExpiresAfter)...)
}
```

5. Add a helper near the top of the file:

```go
// priorAccumulated reads the store from state: accumulated, or outputs
// when state predates the attribute (spec section 9).
func (m setResourceModel) priorAccumulated(ctx context.Context) ([]string, diag.Diagnostics) {
	if m.Accumulated.IsNull() {
		return stringsFromSet(ctx, m.Outputs)
	}
	return stringsFromSet(ctx, m.Accumulated)
}
```

(import `diag` from `github.com/hashicorp/terraform-plugin-framework/diag`.)

6. Replace `Create`:

```go
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

	// Create is the reseed case: no prior store, no prior stamps, so every
	// value enters (spec T4) and the only clock use is stamping visible_at.
	args := stampArgs{
		PlanInputs:     inputs,
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// Known path: realize membership verbatim and resolve the fresh
		// visible_at entries with the apply clock.
		accumulated, d := stringsFromSet(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulated, args, clockNow())
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextSetOutputs(true, inputs, nil)
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, clockNow())
	resp.Diagnostics.Append(d...)
	acc, d := setFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := setFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

7. In `ModifyPlan`: add `plan.DelayedFor.IsUnknown()` to the plan unknown guard and `state.DelayedFor.IsUnknown() || state.Accumulated.IsUnknown()` to the state guard. Immediately after the guards add

```go
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(plan.DelayedFor, plan.ExpiresAfter)...)
	if resp.Diagnostics.HasError() {
		return
	}
```

Replace the variable block and everything from `planInputs, d := ...` to the end of the function with:

```go
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	var priorAccumulated []string
	var prior stamps
	args := stampArgs{
		PlanInputs:     planInputs,
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

	merged := accumulate.NextSetOutputs(reseed, planInputs, priorAccumulated)

	// A reseed discards history: no prior store and no prior stamps, so
	// every value enters with a full delay window (spec T4).
	if !reseed {
		args.PriorAccumulated = priorAccumulated
		args.Prior = prior
	}

	// No clock here (spec section 6): refresh already culled and revealed,
	// so membership is a pure function of state and configuration.
	outputs, detailed, d := planStamps(ctx, merged, args)
	resp.Diagnostics.Append(d...)
	acc, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	out, d := setFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	if creating || replacing {
		plan.ID = types.StringValue(accumulate.HashID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
```

Delete the now-unused `stateOutputs`, `priorStamps`, `priorExpiresAfter` declarations.

8. Replace `Read`:

```go
// Read culls expired values and reveals due ones: the refresh-time half of
// both designs. Read cannot see configuration, so both decisions run on
// stored stamps alone. It writes state only when a value was dropped or
// revealed, or when state predates accumulated and needs it filled from
// outputs (spec section 9), which keeps the plan after a refresh empty.
func (r *setResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state setResourceModel
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

	now := clockNow()
	survivors, expires := accumulate.FilterExpired(values, prior.Expires, now)
	visible, pending := accumulate.FilterPending(survivors, prior.Visible, now)
	if !state.Accumulated.IsNull() && len(survivors) == len(values) && len(pending) == len(prior.Visible) {
		return
	}

	acc, d := setFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	out, d := setFromStrings(ctx, visible)
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
```

9. Replace `Update`:

```go
func (r *setResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
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
		PlanInputs:     planInputs,
		PriorExpires:   int64Ptr(state.ExpiresAfter),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PriorDelay:     int64Ptr(state.DelayedFor),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if !reseed {
		args.PriorAccumulated = priorAccumulated
		args.Prior = prior
	}
	now := clockNow()

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		// Known path: membership and keeper stamps reproduce exactly through
		// the pure functions ModifyPlan used; only fresh entries resolve here.
		accumulated, d := stringsFromSet(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulated, args, now)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextSetOutputs(reseed, planInputs, priorAccumulated)
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, now)
	resp.Diagnostics.Append(d...)
	acc, d := setFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := setFromStrings(ctx, outputs)
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
```

10. In `ImportState`, build `detailed` with `mapFromStamps(ctx, merged, stamps{})` and set `DelayedFor: types.Int64Null()` and `Accumulated: outputs` in the model literal.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/provider/` (the `TestProviderSchema` set half and `TestReadFillsNullAccumulated` now pass; the list half of `TestProviderSchema` still fails until Task 4).
Then: `make testacc TESTARGS='-run "TestAccSet|TestAccImportMalformed"'`
Expected: every set test passes, including the pre-existing expiry tests with `visible_at` null.

- [ ] **Step 5: House checks and commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/resource_set.go internal/provider/resource_set_test.go internal/provider/read_test.go
git commit -m "feat: accumulator_set delayed_for and accumulated"
```

---

### Task 4: `accumulator_list` wiring and acceptance tests

Same wiring as Task 3 with list conversions and the `inputsChanged` branch; this task repeats the code in full so it stands alone.

**Files:**
- Edit: `internal/provider/resource_list.go`
- Test: `internal/provider/resource_list_test.go`
- Edit: `internal/provider/import_test.go` (one assertion in `TestAccListImport`)

**Interfaces:**
- Consumes: the same helpers as Task 3; `NextListOutputs`.
- Produces: `listResourceModel` with `DelayedFor types.Int64` and `Accumulated types.List`; `(*listResource).ValidateConfig`.

- [ ] **Step 1: Write the failing acceptance tests**

Add to `internal/provider/resource_list_test.go`:

```go
func expectListAccumulated(values ...string) statecheck.StateCheck {
	return expectAccumulated("accumulator_list.test", knownvalue.ListExact(stringList(values...)))
}

func expectListPlanAccumulated(values ...string) plancheck.PlanCheck {
	return expectPlanAccumulated("accumulator_list.test", knownvalue.ListExact(stringList(values...)))
}

// TestAccListCreateWithDelayedFor pins D1 on create and that length trims
// accumulated, not outputs: [a, b, c] with length 2 keeps [b, c] pending.
func TestAccListCreateWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(`["a", "b", "c"]`, 2, `  delayed_for = 3600`),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					expectListPlanOutputs(),
					expectListPlanAccumulated("b", "c"),
					expectPlanDetailVisibleUnknown("accumulator_list.test", "b"),
					expectPlanDetailVisibleUnknown("accumulator_list.test", "c"),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs(),
				expectListAccumulated("b", "c"),
				expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
					"b": pendingDetail(), "c": pendingDetail(),
				}),
			},
		}},
	})
}

// TestAccListOutputsKnownAtPlanTimeWithDelayedFor extends the PR #2 suite:
// append, length change, reset, and replacement all plan outputs and
// accumulated as known while delayed_for is set.
func TestAccListOutputsKnownAtPlanTimeWithDelayedFor(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 10, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
			{
				// Append: a stays visible (D5), b enters and hides (D1).
				Config: listConfig(`["b"]`, 10, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a"),
						expectListPlanAccumulated("a", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a"),
					expectListAccumulated("a", "b"),
				},
			},
			{
				// Length-only change re-trims accumulated; b stays pending.
				Config: listConfig(`["b"]`, 1, `  delayed_for = 3600`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs(),
						expectListPlanAccumulated("b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs(), expectListAccumulated("b")},
			},
			{
				// Reset: everything reseeds and enters (T4).
				Config: listConfig(`["b"]`, 1, `  delayed_for = 3600
  triggers_reset = "v1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs(),
						expectListPlanAccumulated("b"),
						expectPlanDetailVisibleUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs(), expectListAccumulated("b")},
			},
			{
				// Replacement: Create reseeds and enters (T4).
				Config: listConfig(`["b"]`, 1, `  delayed_for = 3600
  triggers_reset = "v1"
  triggers_replacement = "r1"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_list.test", plancheck.ResourceActionReplace),
						expectListPlanOutputs(),
						expectListPlanAccumulated("b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs(), expectListAccumulated("b")},
			},
		},
	})
}

// TestAccListRevealRealizedOnRefresh pins V1 with duplicates: both
// occurrences of a reveal together at the refresh, and the plan is empty.
func TestAccListRevealRealizedOnRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: listConfig(`["a", "a"]`, 10, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs(),
					expectListAccumulated("a", "a"),
					expectDetailed("accumulator_list.test", setDetail("a", pendingDetail())),
				},
			},
			{
				Config:    listConfig(`["a", "a"]`, 10, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "a"),
					expectDetailed("accumulator_list.test", setDetail("a", nullDetail())),
				},
			},
		},
	})
}

// TestAccListRevealKeepsHistoryOrder pins the order rule of spec section 7:
// a revealed value lands in its accumulated position, not at the end, and a
// re-appended visible value shares the visible entry.
func TestAccListRevealKeepsHistoryOrder(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 10, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
			{
				// b enters pending: accumulated [a, b], outputs [a].
				Config: listConfig(`["b"]`, 10, `  delayed_for = 1`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a"),
					expectListAccumulated("a", "b"),
				},
			},
			{
				// a re-appends while visible: accumulated [a, b, a], outputs
				// [a, a]. b is still pending only if the plan runs within 1s,
				// so this step asserts accumulated and lets the reveal land
				// whenever the clock says.
				Config: listConfig(`["a"]`, 10, `  delayed_for = 1`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{expectListPlanAccumulated("a", "b", "a")},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListAccumulated("a", "b", "a")},
			},
			{
				Config:    listConfig(`["a"]`, 10, `  delayed_for = 1`),
				PreConfig: sleepPastExpiry(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "a"),
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": nullDetail(), "b": nullDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListDelayedForRemoved pins T2 on the list resource.
func TestAccListDelayedForRemoved(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, `  delayed_for = 3600`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs()},
			},
			{
				Config: listConfig(`["a", "b"]`, 10, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs("a", "b"),
						expectPlanDetailed("accumulator_list.test", map[string]knownvalue.Check{
							"a": nullDetail(), "b": nullDetail(),
						}),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
			},
		},
	})
}

// TestAccListDelayedForZeroRevealsNextRefresh pins delayed_for = 0 on the
// list resource.
func TestAccListDelayedForZeroRevealsNextRefresh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: listConfig(`["a"]`, 10, `  delayed_for = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{expectListPlanOutputs()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs(),
					expectDetailed("accumulator_list.test", setDetail("a", pendingDetail())),
				},
			},
			{
				Config: listConfig(`["a"]`, 10, `  delayed_for = 0`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
			},
		},
	})
}

// TestAccListDelayedForMustBeShorterThanExpiresAfter pins C1 on the list
// resource.
func TestAccListDelayedForMustBeShorterThanExpiresAfter(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      listConfig(`["a"]`, 10, "  delayed_for = 10\n  expires_after = 10"),
			ExpectError: regexp.MustCompile(`delayed_for must be shorter than expires_after`),
		}},
	})
}

// TestAccListPendingLeaverStamped pins T5 on the list resource.
func TestAccListPendingLeaverStamped(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b"]`, 10, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs()},
			},
			{
				Config: listConfig(`["a"]`, 10, "  delayed_for = 3600\n  expires_after = 7200"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						expectListPlanOutputs(),
						expectListPlanAccumulated("a", "b", "a"),
						expectPlanDetailUnknown("accumulator_list.test", "b"),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectDetailed("accumulator_list.test", map[string]knownvalue.Check{
						"a": pendingDetail(), "b": pendingLeaverDetail(),
					}),
				},
			},
		},
	})
}

// TestAccListUnknownDelayedForAtPlan pins the fallback path on the list
// resource.
func TestAccListUnknownDelayedForAtPlan(t *testing.T) {
	config := `
resource "accumulator_list" "src" {
  inputs = [uuid()]
  length = 1
}

resource "accumulator_list" "test" {
  inputs      = ["a"]
  length      = 10
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
					plancheck.ExpectUnknownValue("accumulator_list.test", tfjsonpath.New("outputs")),
					plancheck.ExpectUnknownValue("accumulator_list.test", tfjsonpath.New("accumulated")),
					plancheck.ExpectUnknownValue("accumulator_list.test", tfjsonpath.New("detailed_outputs")),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs(),
				expectListAccumulated("a"),
			},
		}},
	})
}
```

In `internal/provider/import_test.go`, `TestAccListImport`'s settling step: add `expectListAccumulated("a", "b", "c")` to its `ConfigStateChecks`. In `TestAccSetImport`'s settling step add `expectSetAccumulated("a", "b", "d")`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make testacc TESTARGS='-run TestAccListCreateWithDelayedFor'`
Expected: FAIL with an unsupported argument `delayed_for`.

- [ ] **Step 3: Wire the resource**

In `internal/provider/resource_list.go`:

1. Add `_ resource.ResourceWithValidateConfig = (*listResource)(nil)`.

2. Add to `listResourceModel`, after `ExpiresAfter`:

```go
	DelayedFor          types.Int64  `tfsdk:"delayed_for"`
	Accumulated         types.List   `tfsdk:"accumulated"`
```

3. Schema. Extend the resource description with: "With `delayed_for` set, a newly supplied value lands in `accumulated` at once but joins `outputs` only after the delay; see `delayed_for` for the clock rules." Add `delayed_for` with the same description text as Task 3 step 3, and:

```go
			"accumulated": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The accumulated history, oldest first, trimmed to at most `length` " +
					"elements, including values still waiting out `delayed_for`. `outputs` is this " +
					"list minus the pending values, in the same order; with `delayed_for` null the " +
					"two are always equal. Computed; never configured. Known at plan time under the " +
					"same conditions as `outputs`.",
			},
```

`detailed_outputs` takes the Task 3 description. `outputs` description: replace "The accumulated history, oldest first, trimmed to at most `length` elements." with "The visible history, oldest first: `accumulated` minus every value still waiting out `delayed_for`, so it may hold fewer than `length` elements while values are pending." and add `delayed_for` to the unknown list.

4. Add `ValidateConfig` and the `priorAccumulated` helper:

```go
// ValidateConfig is spec C1 on raw configuration: delayed_for must be
// strictly less than expires_after when both are known. Unknown values
// skip the check here; ModifyPlan repeats it once they resolve.
func (r *listResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config listResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(checkDelayShorterThanExpiry(config.DelayedFor, config.ExpiresAfter)...)
}

// priorAccumulated reads the store from state: accumulated, or outputs
// when state predates the attribute (spec section 9).
func (m listResourceModel) priorAccumulated(ctx context.Context) ([]string, diag.Diagnostics) {
	if m.Accumulated.IsNull() {
		return stringsFromList(ctx, m.Outputs)
	}
	return stringsFromList(ctx, m.Accumulated)
}
```

5. Replace `Create`:

```go
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

	// Create is the reseed case: no prior store, no prior stamps, so every
	// value enters (spec T4) and the only clock use is stamping visible_at.
	args := stampArgs{
		PlanInputs:     inputs,
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		accumulated, d := stringsFromList(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulated, args, clockNow())
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	merged := accumulate.NextListOutputs(true, false, inputs, nil, int(plan.Length.ValueInt64()))
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, clockNow())
	resp.Diagnostics.Append(d...)
	acc, d := listFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

6. In `ModifyPlan`: add `plan.DelayedFor.IsUnknown()` to the plan guard and `state.DelayedFor.IsUnknown() || state.Accumulated.IsUnknown()` to the state guard; after the guards add the `checkDelayShorterThanExpiry(plan.DelayedFor, plan.ExpiresAfter)` call with an early return on error. Replace everything from `planInputs, d := ...` to the end with:

```go
	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	var priorAccumulated []string
	var prior stamps
	args := stampArgs{
		PlanInputs:     planInputs,
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
	inputsChanged := !creating && !plan.Inputs.Equal(state.Inputs)

	merged := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, priorAccumulated,
		int(plan.Length.ValueInt64()))

	// A reseed discards history: no prior store and no prior stamps, so
	// every value enters with a full delay window (spec T4).
	if !reseed {
		args.PriorAccumulated = priorAccumulated
		args.Prior = prior
	}

	// No clock here (spec section 6): refresh already culled and revealed,
	// so membership is a pure function of state and configuration.
	outputs, detailed, d := planStamps(ctx, merged, args)
	resp.Diagnostics.Append(d...)
	acc, d := listFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Accumulated = acc
	plan.Outputs = out
	plan.DetailedOutputs = detailed
	if creating || replacing {
		plan.ID = types.StringValue(accumulate.HashID(planInputs))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
```

Delete the unused `stateOutputs`, `priorStamps`, `priorExpiresAfter` declarations.

7. Replace `Read` with the Task 3 body, substituting `listResourceModel`, `listFromStrings`, and the doc comment unchanged otherwise:

```go
func (r *listResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state listResourceModel
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

	now := clockNow()
	survivors, expires := accumulate.FilterExpired(values, prior.Expires, now)
	visible, pending := accumulate.FilterPending(survivors, prior.Visible, now)
	if !state.Accumulated.IsNull() && len(survivors) == len(values) && len(pending) == len(prior.Visible) {
		return
	}

	acc, d := listFromStrings(ctx, survivors)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, visible)
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
```

8. Replace `Update`:

```go
func (r *listResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
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
		PlanInputs:     planInputs,
		PriorExpires:   int64Ptr(state.ExpiresAfter),
		PlannedExpires: int64Ptr(plan.ExpiresAfter),
		PriorDelay:     int64Ptr(state.DelayedFor),
		PlannedDelay:   int64Ptr(plan.DelayedFor),
	}
	if !reseed {
		args.PriorAccumulated = priorAccumulated
		args.Prior = prior
	}
	now := clockNow()

	if !plan.Outputs.IsUnknown() && !plan.Accumulated.IsUnknown() && !plan.DetailedOutputs.IsUnknown() {
		accumulated, d := stringsFromList(ctx, plan.Accumulated)
		resp.Diagnostics.Append(d...)
		detailed, d := applyStamps(ctx, accumulated, args, now)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.DetailedOutputs = detailed
		plan.ID = state.ID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	inputsChanged := !plan.Inputs.Equal(state.Inputs)
	merged := accumulate.NextListOutputs(reseed, inputsChanged, planInputs, priorAccumulated,
		int(plan.Length.ValueInt64()))
	accumulated, outputs, detailed, d := applyFallback(ctx, merged, args, now)
	resp.Diagnostics.Append(d...)
	acc, d := listFromStrings(ctx, accumulated)
	resp.Diagnostics.Append(d...)
	out, d := listFromStrings(ctx, outputs)
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
```

9. In `ImportState`, build `detailed` with `mapFromStamps(ctx, seed.Outputs, stamps{})` and set `DelayedFor: types.Int64Null()` and `Accumulated: outputs` in the model literal.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/...` (all unit tests, including the full `TestProviderSchema`, now pass).
Then: `make testacc`
Expected: the whole acceptance suite passes.

- [ ] **Step 5: House checks and commit**

```bash
gofmt -l . && go vet ./...
git add internal/provider/resource_list.go internal/provider/resource_list_test.go internal/provider/import_test.go
git commit -m "feat: accumulator_list delayed_for and accumulated"
```

---

### Task 5: examples, README, and generated docs

**Files:**
- Edit: `examples/resources/accumulator_set/resource.tf`, `examples/resources/accumulator_list/resource.tf`
- Edit: `README.md`, `templates/index.md.tmpl`
- Regenerate: `docs/index.md`, `docs/resources/list.md`, `docs/resources/set.md`

- [ ] **Step 1: Update the examples**

`examples/resources/accumulator_set/resource.tf`:

```hcl
resource "accumulator_set" "seen_hosts" {
  inputs        = [var.hostname]
  delayed_for   = 600
  expires_after = 3600
}

output "seen_hosts" {
  value = accumulator_set.seen_hosts.outputs
}

output "seen_hosts_all" {
  value = accumulator_set.seen_hosts.accumulated
}

output "seen_hosts_detail" {
  value = accumulator_set.seen_hosts.detailed_outputs
}
```

`examples/resources/accumulator_list/resource.tf`:

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs        = [var.deploy_sha]
  length        = 5
  delayed_for   = 300
  expires_after = 86400
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}

output "recent_deploys_all" {
  value = accumulator_list.recent_deploys.accumulated
}

output "recent_deploys_detail" {
  value = accumulator_list.recent_deploys.detailed_outputs
}
```

- [ ] **Step 2: Update the hand-written prose**

In `README.md`, after the paragraph that starts "Both resources accept an optional `expires_after`", add:

```markdown
Both resources also accept an optional `delayed_for` (seconds). A newly
supplied value is recorded in the computed `accumulated` attribute at once
but joins `outputs` only after `delayed_for` seconds have passed, at the
first refresh after that moment. With `delayed_for` null, `accumulated`
and `outputs` are always equal. `delayed_for` must be strictly less than
`expires_after` when both are set. Each `detailed_outputs` entry carries
`visible_at` beside `expires_at`.
```

In `templates/index.md.tmpl`, extend the sentence that mentions `expires_after` with a parallel clause: "and an optional warm-up window (`delayed_for`) before a new value appears in `outputs`".

- [ ] **Step 3: Regenerate the docs**

Run: `make docs`
Expected: `docs/resources/list.md`, `docs/resources/set.md`, and `docs/index.md` change; `git status` shows only those files plus the edits from steps 1 and 2.

Open `docs/resources/set.md` and check that `delayed_for`, `accumulated`, and the `visible_at` nested attribute all appear with their descriptions.

- [ ] **Step 4: Verify and commit**

```bash
go test ./internal/... && make docs && git status --short
git add examples/resources/accumulator_set/resource.tf examples/resources/accumulator_list/resource.tf README.md templates/index.md.tmpl docs/index.md docs/resources/list.md docs/resources/set.md
git commit -m "docs: document delayed_for and accumulated"
```

---

## Self-review notes

- **Spec coverage.** Section 3 and 4 rules: D1 to D5 and T2 in Task 1 (`ClassifyDelays`, `ApplyDelay`); V1 in Task 1 (`FilterPending`) and the Read bodies of Tasks 3 and 4; T4 reseed handling in the `if !reseed` blocks of Tasks 3 and 4 plus the "reseed ignores prior stamps" unit case; T5 in `TestAccSetPendingLeaverStamped` / `TestAccListPendingLeaverStamped`; C1 in Task 2 (`checkDelayShorterThanExpiry`) wired through `ValidateConfig` and ModifyPlan in Tasks 3 and 4. Section 5 schema in Tasks 3 and 4 plus `TestProviderSchema` in Task 2. Section 6 known-at-plan invariant in the `OutputsKnownAtPlanTimeWithDelayedFor` tests. Section 8 import in Tasks 3 and 4 `ImportState` plus the import tests. Section 9 compatibility in `priorAccumulated`, `stampsFromMap`'s missing-key tolerance, `TestStampsFromMapToleratesMissingVisibleAt`, `TestStateGetToleratesMissingDelayAttributes`, and `TestReadFillsNullAccumulated`. Section 11 docs in Task 5.
- **Type consistency.** `stamps`, `classified`, `stampArgs`, `planStamps`, `applyStamps`, `applyFallback`, `checkDelayShorterThanExpiry`, `priorAccumulated` carry the same names in Tasks 2, 3, and 4. `FreshStamp` stays unchanged.
- **Review Focus.** Each of the five lines names its pinning test and that test exists in the named task.
- **Known gap, accepted.** Step 2 of `TestAccSetOutputsKnownAtPlanTimeWithDelayedFor` (a stays visible, b enters) covers the spec's "`delayed_for` set on existing resource" row rather than a separate test. The spec's "increase" row observes only that the value stays pending with a fresh stamp; an exact max() check needs a fixed clock, which the unit table in Task 1 covers.
