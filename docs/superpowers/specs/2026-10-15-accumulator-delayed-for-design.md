# accumulator delayed_for — design

Date: 2026-10-15
Status: approved

## 1. Motivation

`expires_after` (spec of 2026-09-23) lets a value leave `outputs` on its
own once nobody supplies it any more. Nothing comparable exists on the way
in: a value shows up in `outputs` at the apply that adds it to `inputs`.
Some consumers want the opposite of a TTL, a warm-up window: the resource
should record a newly seen host, branch, or SHA immediately but only let
it influence downstream resources after it has stuck around for a while.

This feature adds a per-resource delay:

- an optional `delayed_for` attribute (seconds), and
- a filter between accumulation and `outputs`: the resource accumulates a
  value as soon as the configuration supplies it, but keeps it out of
  `outputs` until `delayed_for` has elapsed since it entered.

The delay is lazy, like expiry: state is the only store, so a value becomes
visible only when a refresh runs after its time has passed.

## 2. Scope

**In scope:** the `delayed_for` attribute, the new computed `accumulated`
attribute, the `visible_at` attribute inside `detailed_outputs`, the
`delayed_for < expires_after` check, pure-function support in
`internal/accumulate`, unit and acceptance tests, regenerated
documentation, and examples. Everything lands on both resources, with the
same shape on each.

**Out of scope (deferred):**

- Import support for `visible_at`. Imported values seed visible.
- Any scheduler or background reveal. Values become visible only at
  refresh.

## 3. Concepts

- **`delayed_for`** — optional `int64`, seconds, on both resources.
  `int64validator.Between(0, maxExpiresAfter)` bounds it, the same
  overflow bound as `expires_after`. `null` disables the delay entirely.
- **`accumulated`** — computed; the full accumulated history. On
  `accumulator_list` a list, oldest first, trimmed to `length`, with
  duplicates. On `accumulator_set` a set. This is exactly what `outputs`
  holds today: `NextListOutputs` / `NextSetOutputs` now produce
  `accumulated`. Always concrete, `[]` never null.
- **`outputs`** — computed; the visible subset of `accumulated`, in
  `accumulated`'s order on the list resource. With `delayed_for` null
  throughout, `outputs` always equals `accumulated`.
- **`visible_at`** — the per-value reveal timestamp, RFC 3339, UTC, second
  precision. Each `detailed_outputs` entry stores it as a second attribute
  beside `expires_at`. Non-null means the value is pending and hidden from
  `outputs`. Null means the value is visible.
- **`detailed_outputs`** — computed map from every value in `accumulated`
  (no longer only `outputs`) to `{ expires_at, visible_at }`. One entry per
  distinct value. Always concrete, `{}` never null.
- **The one invariant.** `outputs` equals `accumulated` filtered to the
  values whose `visible_at` is null. Every phase preserves it.
- **A value "enters"** when the next `accumulated` holds it and the prior
  `accumulated` did not. On the list resource, entry is per distinct
  value: re-appending a value that is already present (visible or pending)
  does not enter again, and all occurrences share the one entry.
- **Only refresh and apply read the clock, never plan.** Unchanged from
  the expiry design (section 6 there). Refresh reveals pending values; the
  apply that creates a `visible_at` stamps it.

## 4. Semantics

"Now" means the clock of the phase the rule names: refresh for reveal,
apply for stamping, never the plan.

**Membership rules:**

- **A1 (accumulate).** The next `accumulated` follows the same algorithm
  that produces `outputs` today: reseed / append / union / trim, against
  the prior `accumulated`. Trim happens here; the delay filters afterward
  and can never resurrect a trimmed value.
- **A2 (visible subset).** The next `outputs` equals the next
  `accumulated` minus every value that stays pending after this apply
  (rules D1 through D5 decide which). This is a pure function of prior
  state and configuration: ModifyPlan computes it without a clock, and the
  plan shows it fully known whenever it knows the branch inputs (section
  6).

**Stamping rules (apply when the configuration sets `delayed_for`; "now"
means the apply clock of the apply performing the stamp):**

- **D1 (entering).** A value that enters `accumulated` gets
  `visible_at = now + delayed_for` and stays out of `outputs`. The entry
  shows as unknown in that apply's plan and the apply stamps it. This
  includes create and reseed: every value seeded from `inputs` on a
  create, a replacement, or a `triggers_reset` change enters, so a fresh
  resource with `delayed_for` set starts with empty `outputs`.
- **D2 (`delayed_for` unchanged, pending value).** The prior `visible_at`
  survives unchanged. A pending value's clock stays frozen from the apply
  that stamped it (or from the last `delayed_for` change).
- **D3 (`delayed_for` decreased).** Every pending value gets a candidate
  `now + delayed_for` and keeps `min(old, candidate)`.
- **D4 (`delayed_for` increased).** Every pending value gets a candidate
  `now + delayed_for` and keeps `max(old, candidate)`.
- **D5 (already visible).** A value with a null `visible_at` that the
  prior `accumulated` holds stays visible. Setting or changing
  `delayed_for` never hides a value that `outputs` already shows.

**Reveal rule:**

- **V1 (refresh reveals pending values).** Read clears every `visible_at`
  that is non-null and not strictly after the refresh clock, which by the
  invariant adds that value to `outputs` in its `accumulated` position.
  Read culls expired values first (X1 of the expiry design), then
  reveals; a value that is both expired and due disappears, never shows.
  `delayed_for = 0` thus means: the apply where a value enters stamps it
  with the current moment, that apply leaves `outputs` unchanged, and the
  next refresh reveals it.

**Transition rules:**

- **T1 (`delayed_for` null throughout).** Nothing is ever pending; every
  `visible_at` is null; `outputs` equals `accumulated` after every phase.
  Behavior matches the provider today, plus the new attributes.
- **T2 (`delayed_for` removed).** The next apply clears every `visible_at`
  and every pending value appears in `outputs` at that apply. This needs
  no clock, so the plan shows the revealed `outputs` as known.
- **T3 (`delayed_for` set or changed).** D1 through D5 apply at that
  apply. Setting it on an existing resource changes nothing until the next
  value enters (D5).
- **T4 (reset or replacement).** `accumulated` reseeds from `inputs`;
  every seeded value enters (D1) and stays pending when the configuration
  sets `delayed_for`. Every `expires_at` is null, as today.
- **T5 (pending value leaves `inputs`).** The value keeps its `visible_at`
  and gains an `expires_at` under the expiry rules (S1). Because the check
  forces `delayed_for < expires_after`, `visible_at` always precedes
  `expires_at` for a value stamped under one configuration: it becomes
  visible before it can expire. After a configuration change the two may
  cross; V1's cull-then-reveal order handles that.
- **T6 (pending value re-supplied).** A pending value that stays in or
  returns to `inputs` keeps its `visible_at`; on the list resource a
  re-append adds an occurrence that shares the pending entry.

**Configuration check:**

- **C1 (`delayed_for` must be shorter than `expires_after`).** When the
  configuration sets both attributes to known non-null values and
  `delayed_for >= expires_after`, the provider reports an error on
  `delayed_for` during configuration validation. An unknown value on
  either side skips the check there; ModifyPlan repeats it once it knows
  both values, so a pair that resolves to a bad combination still fails at
  plan rather than at apply.

**Worked example** (`accumulator_set`, `delayed_for = 300`,
`expires_after = 900`, plan and apply back to back):

| Plan/apply | `inputs` | `accumulated` after | `outputs` after | `detailed_outputs` after |
| --- | --- | --- | --- | --- |
| t0 = 10:00 | `["a","b"]` | `{a, b}` | `{}` | `a: visible 10:05`, `b: visible 10:05` |
| t1 = 10:02 | `["a","c"]` | `{a, b, c}` | `{}` | `a: visible 10:05`; `b: visible 10:05, expires 10:17`; `c: visible 10:07` |
| t2 = 10:06 | `["a","c"]` | `{a, b, c}` | `{a, b}` | `a: null`; `b: expires 10:17`; `c: visible 10:07` (t2's refresh reveals a and b; plan empty) |
| t3 = 10:08 | `["a","c"]` | `{a, b, c}` | `{a, b, c}` | `a: null`; `b: expires 10:17`; `c: null` (t3's refresh reveals c; plan empty) |
| t4 = 10:20 | `["a","c"]` | `{a, c}` | `{a, c}` | `a: null`; `c: null` (t4's refresh culls b) |

At t0 both values enter and the apply stamps them (D1); `outputs` is empty
and known at plan. At t1, `b` leaves `inputs` while pending and gains an
`expires_at` (T5); `c` enters. At t2's refresh, `10:06 >= 10:05`, so Read
reveals `a` and `b` and the plan that follows is empty. `c` follows at t3.
At t4, `b` expires exactly as it would today.

## 5. Schema changes

Both resources gain the same attributes, with identical descriptions
except for list/set wording:

| Attribute | Type | Required/Optional/Computed | Validators |
| --- | --- | --- | --- |
| `delayed_for` | `int64` | Optional | `int64validator.Between(0, maxExpiresAfter)` |
| `accumulated` | list(string) on list, set(string) on set | Computed | none |
| `detailed_outputs` | `map(string, object({expires_at = string, visible_at = string}))` | Computed | none (shape change) |

Notes:

- `delayed_for` has no plan modifier. A change is an in-place update; it
  never reseeds, never replaces.
- `accumulated` has no `UseStateForUnknown`, for the same reason `outputs`
  has none (PR #2): ModifyPlan computes it whenever it knows the branch
  inputs, and copying prior state would give the wrong answer on every
  apply that changes membership.
- `detailedObjectType` gains `visible_at`. Prior state whose entries lack
  the attribute unmarshals with it null, which means visible (section 9).
- `id` hashing stays the same.
- Both resources add a `ValidateConfig` method
  (`resource.ResourceWithValidateConfig`) for C1.

## 6. Plan/apply mechanics

The invariant inherited from PR #2 and from the expiry design, stated
once:

> **The plan shows `outputs` and `accumulated` fully known whenever it
> knows the branch inputs.** Membership is a pure function of prior state
> and configuration. The only unknown planned values are the `visible_at`
> and `expires_at` attributes of entries this apply will stamp. The branch
> inputs are `inputs` (every element), `length`, both triggers,
> `expires_after`, and now `delayed_for`; when any of them is unknown, or
> any mirrored state attribute is unknown, ModifyPlan leaves `outputs`,
> `accumulated`, and `detailed_outputs` unknown and the apply resolves
> them.

Mechanically:

- **Read (refresh) culls, then reveals.** Read parses `accumulated`,
  `outputs`, and `detailed_outputs`; drops expired values from all three
  (X1); clears due `visible_at` stamps (V1); recomputes `outputs` from the
  invariant; and writes state back only when it dropped or revealed a
  value. Read never sees configuration, so both decisions run on stored
  stamps alone.
- **ModifyPlan reads no clock.** On the known path it computes the next
  `accumulated` through the existing shared functions against the prior
  `accumulated`; classifies every value as entering, pending, or visible
  (D1 through D5, T2) without a clock; writes `outputs` as the visible
  subset, known; writes `accumulated`, known; and writes `detailed_outputs`
  with known keys, null or known-prior stamps for keepers, and unknown
  `visible_at` / `expires_at` for entries this apply stamps. OpenTofu's
  apply-time re-derivation reproduces every known value exactly.
- **Apply stamps fresh entries.** Create and Update on the known path
  reproduce membership through the same pure functions and resolve each
  unknown stamp with the apply clock. Create now stamps: under
  `delayed_for`, every seeded value enters (D1). The create plan still
  shows `outputs` (empty) and `accumulated` (the seeded values) as known.
- **Fallback path, apply clock everywhere.** When ModifyPlan cannot
  compute, the plan promises "(known after apply)" for `outputs`,
  `accumulated`, and `detailed_outputs`, and Create or Update computes
  membership and both stamps with its own clock.
- **When an update runs.** Only when the plan differs from state: an
  `inputs` change, a trigger change, an `expires_after` or `delayed_for`
  change, or fresh entries resolving. A reveal never plans an update:
  refresh already moved the value into `outputs`, so the plan that follows
  is empty. A steady state with `delayed_for` set produces an empty plan.
- **Saved plans** applied late stamp their fresh entries from the apply
  clock and reveal nothing their own refresh did not reveal; same
  limitation and same reasoning as expiry.
- **Destroy plans** keep their current behavior.

## 7. Algorithm

New pure functions in `internal/accumulate`, in a new file `delay.go`,
with the clock injected:

```
// FilterPending partitions values into visible and pending by their
// stored visible_at stamps (V1 after the reveal, A2): a value with no
// stamp, or a stamp not strictly after now, is visible; the rest are
// pending. Order is preserved. Returned stamps hold only the pending
// values' stamps, so the caller can write the cleared entries back as
// null. Read calls it with the refresh clock.
FilterPending(values []string, stamps map[string]time.Time,
    now time.Time) (visible []string, pending map[string]time.Time)

// ClassifyDelays decides each value of next without a clock (D1 to D5,
// T2). keep holds prior pending stamps that survive unchanged
// (plannedDelayedFor set and equal to priorDelayedFor); fresh holds
// entering values (not in prior) plus pending values whose stamp must be
// recomputed (delayedFor changed). When plannedDelayedFor is nil nothing
// is pending: keep and fresh are empty and every value is visible (T2).
// Values in prior with no prior stamp are visible (D5) and appear in
// neither map.
ClassifyDelays(next, prior []string,
    priorStamps map[string]time.Time,
    priorDelayedFor, plannedDelayedFor *int64,
) (keep map[string]time.Time, fresh map[string]struct{})

// Visible returns next minus the values present in any hidden set,
// preserving order. It is the invariant of section 3 as a function;
// ModifyPlan, Create, Update, and Read all derive outputs through it.
Visible(next []string, hidden ...map[string]struct{}) []string
```

The candidate / min / max arithmetic (D1, D3, D4) reuses `FreshStamp`: it
takes the prior stamp map and the two `*int64` values, which have the
same shape for delay as for expiry. The fallback path gets an `ApplyDelay`
sibling of `ApplyExpiration`, or one `ApplyStamps` call that composes the
two; the implementation plan picks the shape, this section fixes the
behavior.

Order of operations, per phase:

1. **Refresh (Read):** `FilterExpired` over `accumulated` with the refresh
   clock; then `FilterPending` over the survivors; `outputs` = the visible
   result; write back only when something changed.
2. **Plan (ModifyPlan, known path):** next `accumulated` via
   `NextListOutputs` / `NextSetOutputs` against the prior `accumulated`;
   `ClassifyStamps` for expiry (unchanged, now over `accumulated`);
   `ClassifyDelays` for delay; `outputs` = `accumulated` minus keep minus
   fresh of the delay classification; `detailed_outputs` with unknown
   attributes for the two fresh sets.
3. **Apply (Create and Update, known path):** reproduce membership through
   the same functions, resolve each fresh stamp with `FreshStamp` and the
   apply clock. The fallback path does the same plus the expiry cull with
   its own clock.

Resource-specific notes:

- **`accumulator_list`.** `accumulated` keeps order and duplicates and is
  what `length` trims; `outputs` is the order-preserving filter and may be
  shorter than `length`. All occurrences of a value share one entry and
  reveal together.
- **`accumulator_set`.** `accumulated` and `outputs` are both sets;
  `accumulated` duplicates the key set of `detailed_outputs`, accepted for
  a uniform shape across the two resources.
- **Expiry interaction.** Expiry classification (`ClassifyStamps`) now
  runs over `accumulated`, so a pending value that leaves `inputs` gets an
  `expires_at` like any other leaver (T5).

## 8. Import

Unchanged contract:

- The import ID format and `ParseImport` stay as they are. Import offers
  no way to supply `visible_at`.
- Imported state seeds `accumulated` equal to `outputs`, and
  `detailed_outputs` with null `expires_at` and null `visible_at` for every
  value. A matching configuration still produces an empty first plan; with
  `delayed_for` set, nothing is pending until the next value enters (D5).

## 9. State compatibility

Additive. Prior state lacks `accumulated` and `delayed_for`, and its
`detailed_outputs` entries lack `visible_at`. Rules for reading it:

- The provider treats a null `accumulated` in state as equal to
  `outputs`. The next plan or refresh that writes state writes the
  concrete value.
- The provider treats a `detailed_outputs` entry without `visible_at` as
  null, that is, visible. `stampsFromMap` grows a second accessor for
  `visible_at` and tolerates its absence.
- `delayed_for` null is T1.

No `UpgradeState` implementation. A unit test unmarshals a raw state
object in the pre-feature shape into the resource model and asserts the
new attributes land null without error, as the expiry spec did; an
acceptance test exercises a Read over state whose `accumulated` is null.

## 10. Testing

### 10.1 Unit tests (`internal/accumulate`)

Table-driven tests for the new functions:

- `FilterPending`: reveal at `now == visible_at` (boundary inclusive);
  keep future stamps pending; unstamped values visible; nil stamp map;
  empty input; order preserved; duplicates share one decision.
- `ClassifyDelays`: entering value fresh (D1); pending value keep under an
  unchanged delay (D2); pending value fresh under a changed delay (D3,
  D4); visible prior value in neither map (D5); every value visible under
  a null planned delay (T2); nil prior map (create and reseed: everything
  enters).
- `FreshStamp` reuse: no new arms, existing tests cover min / max.
- fallback composition: entering value stamped `now + D` and hidden;
  D removed reveals everything; D = 0 leaves the value hidden at the same
  call (stamp equals now, reveal is refresh's job); expiry cull wins over a
  due reveal; nil prior map.

### 10.2 Acceptance tests (`internal/provider`)

Each test runs on both resources unless noted.

| Test | What it asserts |
| --- | --- |
| create with `delayed_for` | plan shows `outputs` known and empty, `accumulated` known and seeded, `visible_at` entries unknown; state after apply matches; `outputs` empty |
| `outputs` known at plan on every branch with `delayed_for` set | create, append / union, length change (list), reset, replacement: `outputs` and `accumulated` known in the plan (extends the PR #2 suite) |
| reveal realized on bare refresh | no config change; a short delay elapses; the step's refresh reveals the value; the plan is empty; `outputs` contains it; `visible_at` null |
| entering value hidden on inputs change | existing visible values stay in `outputs`; the new value appears in `accumulated` only |
| `delayed_for` set on existing resource | existing values stay visible (D5); the next new value waits |
| `delayed_for` removed | pending values revealed at that apply; plan shows revealed `outputs` known (T2) |
| `delayed_for` decreased / increased | min / max observable in `visible_at` |
| `delayed_for` = 0 | hidden at the apply that adds it, revealed by the next refresh |
| `delayed_for >= expires_after` | rejected during configuration validation on `delayed_for`; equal and greater both rejected; `delayed_for < expires_after` accepted |
| pending leaver gets `expires_at` | T5: both stamps present; later revealed, later expired |
| reset / replacement with `delayed_for` set | all seeded values pending again (T4) |
| list order on reveal | a value revealed after later visible values lands in its history position, duplicates reveal together |
| unknown `delayed_for` at plan | `outputs`, `accumulated`, `detailed_outputs` unknown at plan, resolved at apply |
| steady state with `delayed_for` set | second plan after apply is empty, pending or not |
| import with `delayed_for` | seeded values visible; first new value waits |
| pre-feature state | state without `accumulated` refreshes and plans empty against matching config |

### 10.3 House tests

`TestProviderSchema` extends to `delayed_for`, `accumulated`, and the
`visible_at` object attribute on both resources; the em-dash and SPDX
rules apply unchanged.

## 11. Documentation

- `make docs` regenerates `docs/resources/list.md` and `set.md`; CI's
  dirty-tree check enforces it.
- The `delayed_for`, `accumulated`, `outputs`, and `detailed_outputs`
  descriptions state the clock rule in plain language: refresh realizes
  reveals; entries that an apply stamps show as known after apply; a
  late-applied saved plan reveals nothing; `-refresh=false` defers the
  reveal; `delayed_for` must be less than `expires_after` when both are
  set.
- `examples/resources/*/resource.tf` add `delayed_for` and an
  `accumulated` output.

## 12. Decisions and rejected alternatives

- **`outputs` is the filtered view, `accumulated` is the store.** The
  request says `outputs` must not change until the delay has passed, so a
  new attribute must hold the full history. Chosen over private state
  (hidden from users, awkward to debug and import) and over a
  `pending_outputs` attribute (loses list order for revealed values).
- **`accumulated` on both resources.** The set resource could derive it
  from `detailed_outputs` keys. The project owner chose uniform shape
  across the two resources.
- **Null `visible_at` means visible.** Read clears the stamp on reveal
  instead of keeping it as a record, so membership stays a clock-free
  function of state (ModifyPlan can never compare a stamp to a clock) and
  the invariant is a single rule. Mirrors "null `expires_at` means cannot
  expire".
- **Delay on create and reseed.** Every entry passes through the same
  filter, including the initial seed. Chosen for one mental model over an
  immediate first seed.
- **Min / max on `delayed_for` changes.** Same rule as `expires_after`,
  reusing `FreshStamp`; a decrease can only reveal sooner, an increase
  only later, for values still pending when the change applies.
- **Check at configuration validation and at plan.** `ValidateConfig`
  catches a literal bad pair before any plan; ModifyPlan repeats the check
  for pairs that were unknown earlier so the failure is still a plan
  error.
- **Cull before reveal in Read.** A value due for both disappears, never
  shows.
- **Trim first, delay second.** Same reasoning as expiry: the filter can
  never resurrect a trimmed value.

## 13. Limitations

- Reveal is lazy. A pending value stays hidden until some refresh runs
  after its `visible_at`; there is no scheduler.
- A saved plan applied late reveals nothing its own refresh did not
  already reveal.
- `outputs` on the list resource may hold fewer than `length` values
  while values are pending.
- Duplicates in `accumulator_list` share one entry and reveal together.
- The clock is the machine running the refresh or apply; stamps are
  wall-clock arithmetic with no cross-machine synchronization.
