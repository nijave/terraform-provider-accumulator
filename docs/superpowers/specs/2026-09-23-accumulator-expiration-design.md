# accumulator expiration — design

Date: 2026-09-23
Status: approved, revised 2026-09-24

**Revision 2026-09-24 — refresh-time culling.** The original section 6 made
ModifyPlan read the wall clock and write the resulting timestamps as known
planned values. That design cannot work on this platform: OpenTofu calls
`PlanResourceChange`/ModifyPlan twice per apply (once for the saved plan,
once during apply to derive the final planned state) and requires every
value known in the first plan to be identical in the second, so any
`time.Now()` result landing in a known planned value fails intermittently
with "Provider produced inconsistent final plan". The design now follows
the `time_rotating` resource in hashicorp/terraform-provider-time: the
clock is read only in Read (refresh-time culling of expired values) and in
Create/Update when stamping entries born at apply (unknown at plan).
ModifyPlan never reads the clock. Semantic consequences accepted by the
project owner: an `expires_after` increase can no longer resurrect a value
that already expired (refresh culls it first — Read cannot see
configuration); and a value whose time has passed is culled at the first
refresh even if that same run removes `expires_after` from configuration.

## 1. Motivation

`accumulator_list` and `accumulator_set` never forget: a value stays in
`outputs` until a `triggers_reset` change or a replacement discards history.
That is wrong for values with a natural lifetime. A rotating deploy SHA, a
recently-seen node, or an ephemeral branch name should drop out of the
accumulator on its own after it stops being supplied, without someone editing
configuration to evict it.

This feature adds a per-resource TTL:

- an optional `expires_after` attribute (seconds), and
- a computed `detailed_outputs` map that reports, for every accumulated value,
  when that value will expire (`expires_at`, RFC 3339, or null when it cannot
  expire).

Expiration is lazy, like everything else in this provider: state is the only
store, so a value is removed only when a plan/apply runs after its time has
passed.

## 2. Scope

**In scope:** the two attributes on both resources, the plan-time clock
machinery in ModifyPlan/Update/Create, pure-function support in
`internal/accumulate`, unit and acceptance tests, regenerated documentation,
and examples.

**Out of scope (deferred, subject to change in a future version):**

- Import support for `expires_at`. The import ID format and the import
  contract are unchanged. Imported values carry null `expires_at`; the normal
  transition rules stamp them on the first apply when `expires_after` is set
  in configuration (section 4, rule S5).
- Any scheduler or background expiry. Expiration happens only at plan/apply.

## 3. Concepts

- **`expires_after`** — optional `int64`, seconds, on both resources.
  Validated with `int64validator.Between(0, maxExpiresAfter)`, where the
  upper bound (`math.MaxInt64 / int64(time.Second)`, about 292 years) is the
  largest value that `time.Duration(expires_after) * time.Second` can hold
  without overflowing. `null` disables expiration entirely.
- **`expires_at`** — the per-value expiration timestamp, RFC 3339, UTC,
  second precision (`2026-09-23T10:07:00Z`). Stored as the single attribute
  of each `detailed_outputs` entry. Null means the value cannot expire.
- **`detailed_outputs`** — computed map from accumulated value to
  `{ expires_at = <RFC 3339 or null> }`. One entry for every member of
  `outputs`, no more, no fewer. Always concrete: an empty accumulator yields
  `{}`, never null (the same rule that makes `outputs` `[]` rather than
  null).
- **A value is "in `inputs`"** when it is a member of the planned `inputs`
  for the plan being made. Values in `inputs` cannot expire.
- **The clock is read at refresh and at apply, never at plan.** Refresh
  culls expired values (section 6); the apply that creates a timestamp
  stamps it. Planned values are pure functions of prior state and
  configuration. Section 6 covers what this means for saved plans.

## 4. Semantics

The rules below are evaluated in one pass over the accumulated result, in the
order shown. "Now" is the clock of the phase named by the rule — refresh for
removal, apply for stamping — never the plan.

**Membership rules:**

- **R1 (accumulate).** The next `outputs` is computed exactly as today:
  reseed / append / union / trim per the existing algorithm (spec of
  2026-09-17, section 6). Expiration never resurrects a trimmed value and
  never feeds the accumulation: trim happens first, expiration filters
  afterward.
- **R2 (in `inputs` cannot expire).** A value in the planned `inputs` is
  never removed by expiration, and its `expires_at` is null. Because
  in-`inputs` values never carry a stamp, the refresh cull (which cannot
  see configuration) can never remove one either.

**Stamping rules (apply to values not in `inputs`, and only when
`expires_after` is set; "now" is the apply clock of the apply performing
the stamp):**

- **S1 (leaving `inputs`).** A value that is in the accumulated result but
  not in the planned `inputs`, and has no prior stamp, gets `expires_at =
  now + expires_after`. The entry is unknown in that apply's plan and is
  stamped at apply.
- **S2 (`expires_after` unchanged, prior stamp exists).** The prior stamp is
  kept unchanged. Values not in `inputs` never gain a fresh stamp while
  `expires_after` sits still; their clock is frozen from the apply that
  stamped them (or from the last `expires_after` change).
- **S3 (`expires_after` decreased).** Every such value gets a candidate
  `now + expires_after` and keeps `min(old, candidate)`.
- **S4 (`expires_after` increased).** Every such value gets a candidate
  `now + expires_after` and keeps `max(old, candidate)`.
- **S5 (no prior stamp).** A value with no old stamp — history that
  accumulated before `expires_after` was set, or a value that left `inputs`
  while `expires_after` was null — just gets the candidate `now +
  expires_after`. Setting `expires_after` therefore gives every non-input
  value a full window; nothing is purged at the moment it is enabled.

**Removal rule:**

- **X1 (refresh culls expired values).** Read drops every value whose
  stored `expires_at` is non-null and not strictly after the refresh clock.
  Two consequences, both accepted by the project owner with the 2026-09-24
  revision:
  - an `expires_after` increase cannot resurrect a value that already
    expired — refresh culls it before any plan could apply S4, because Read
    cannot see configuration;
  - a value whose time has passed is culled at the first refresh even if
    that same run removes `expires_after` from configuration.
  `expires_after = 0` therefore means: the apply where a value leaves
  `inputs` stamps it with the current moment, and the next refresh removes
  it.

**Transition rules:**

- **T1 (`expires_after` null throughout).** No value ever expires; every
  `expires_at` is null. Behavior is identical to the provider today, plus
  the all-null `detailed_outputs`.
- **T2 (`expires_after` removed).** The next apply expires nothing going
  forward and sets every remaining `expires_at` back to null. (A value
  whose time had already passed is culled by that run's refresh before the
  plan ever sees the configuration change — see X1.)
- **T3 (`expires_after` set or changed).** S1 through S5 apply at that
  apply; in-inputs values stay null.
- **T4 (reset or replacement).** History is discarded and `outputs` is
  reseeded from `inputs` alone, so every `detailed_outputs` entry is null
  again. Create needs no clock at all for the same reason: on create every
  value comes from `inputs`.
- **T5 (re-supply).** A value whose stamp has passed is re-added seamlessly
  by reappearing in `inputs`: even if refresh culled it, the plan unions it
  back in, and R2 gives it a null stamp and no expiry.

**Worked example** (`accumulator_set`, `expires_after = 300`, plan and apply
back to back):

| Plan/apply | `inputs` | `outputs` after | `detailed_outputs` after |
| --- | --- | --- | --- |
| t0 = 10:00 | `["a","b"]` | `{a, b}` | `a: null`, `b: null` |
| t1 = 10:02 | `["a","c"]` | `{a, b, c}` | `a: null`, `b: 10:07`, `c: null` |
| t2 = 10:08 | `["a"]` | `{a, c}` | `a: null`, `c: 10:13` (b culled at t2's refresh) |
| t3 = 10:10 | `["a"]` | `{a, c}` | unchanged; the plan is empty |

At t1, `b` leaves `inputs` and its entry is stamped `t1 + 300 = 10:07` at
the apply (S1; the entry was unknown in t1's plan). At t2's refresh,
`10:08 >= 10:07`, so `b` is culled from state; the plan unions `["a"]` into
what remains, and `c`'s entry is stamped `10:13` at the apply (S1 again).
At t3 nothing has expired, the plan equals state, and Update does not run:
the empty-plan invariant holds even with `expires_after` set.

**`expires_after` change example** (value `d` not in `inputs`, old stamp
10:50, plan at 10:20):

- decreased to 300 (5 minutes): candidate 10:25, `min(10:50, 10:25) =
  10:25`, so `d` now expires at 10:25;
- increased to 3600 (1 hour): candidate 11:20, `max(10:50, 11:20) = 11:20`;
- increased to 600 (10 minutes): candidate 10:30, `max(10:50, 10:30) =
  10:50`, the old stamp wins because the longer old window still ends later;
- a second value `e` stamped 10:15 (already passed) is culled at the next
  refresh; an increase cannot bring it back (X1's revision), and min/max
  only re-times values that are still alive when the change applies.

## 5. Schema changes

Both resources gain the same two attributes:

| Attribute | Type | Required/Optional/Computed | Validators |
| --- | --- | --- | --- |
| `expires_after` | `int64` | Optional | `int64validator.Between(0, maxExpiresAfter)` |
| `detailed_outputs` | `map(string, object({expires_at = string}))` | Computed | none |

Notes:

- `expires_after` has no plan modifier. A change to it is an in-place
  update; it never reseeds, never replaces.
- `detailed_outputs` is a computed map of a single-attribute object. A null
  `expires_at` inside an entry is valid and expected; it is how "cannot
  expire" is represented.
- `id` hashing is unchanged (`inputs` only, at create/import).
- Everything not listed here is unchanged, including `triggers_reset` and
  `triggers_replacement`.

## 6. Plan/apply mechanics

The clock rule and its consequences, stated once, plainly:

> **The clock is read only at refresh (culling) and at apply (fresh
> stamps).** Expired values are removed by `Read` using the refresh clock.
> A timestamp is created only by the apply that needs it, and appears as
> "(known after apply)" in that apply's plan. ModifyPlan never reads the
> clock, so every known planned value is a pure function of prior state and
> configuration, and the plan OpenTofu re-derives at apply is identical to
> the saved one. **A late-applied plan will not expire an item even if the
> time has passed by the time the plan is applied — its refresh already ran
> at plan time; a new plan/apply is required to realize that expiration.**
> This sentence, or its equivalent, appears in the schema documentation for
> both attributes. Running with `-refresh=false` defers culling until the
> next refresh that does run.

Mechanically (the `time_rotating` pattern, adapted from per-resource
rotation to per-value expiry):

- **Read (refresh) culls.** Read drops every value whose stored
  `expires_at` is non-null and not strictly after the refresh clock,
  from both `outputs` and `detailed_outputs`, and writes the filtered
  state back. This is the per-value analogue of `time_rotating`'s
  `Read` calling `resp.State.RemoveResource` when the rotation deadline
  has passed. Read cannot see configuration, so the cull runs on stored
  stamps alone; values in `inputs` are never culled because they never
  carry a stamp (R2).
- **ModifyPlan is clock-free.** When all decision inputs are known,
  ModifyPlan computes `outputs` membership (fully known — the state it
  reads was already culled at refresh) and builds `detailed_outputs`
  with null entries for values in `inputs`, known prior stamps for
  keepers (S2), and unknown entries for fresh values (S1, S3, S4, S5)
  that the apply will stamp. Both invocations of OpenTofu's double plan
  produce identical known values, because nothing time-dependent is
  written as a known value.
- **Apply stamps fresh entries.** Update (known path) reproduces the
  plan's membership and keepers through the same shared pure functions
  and resolves each unknown fresh entry with the apply clock. A saved
  plan applied late stamps its fresh entries from the apply-time clock —
  permitted, because the plan promised unknown for exactly those
  entries. Create never stamps (T4: every value comes from `inputs`).
- **Fallback path, apply clock everywhere.** When ModifyPlan cannot
  compute (any unknown among `inputs` elements, `length`, a trigger,
  `expires_after` itself, or the mirrored state unknowns), the plan
  promises "(known after apply)" exactly as today, and Update computes
  membership, stamps, and removals with its own clock.
- **When does an update run at all.** Only when the plan differs from
  state: an `inputs` change, a trigger change, an `expires_after`
  change, fresh entries resolving, or a fallback-unknown plan. Expiry
  itself no longer plans an update — refresh already removed the value,
  so the plan that follows is empty. A steady state with `expires_after`
  set produces an empty plan (invariant 1 of the 2026-09-17 spec,
  preserved).
- **Saved plans** carry plan-time membership and unknown fresh entries;
  applying them late cannot error, because the re-derived final plan
  reproduces every known value bit-for-bit.
- **Destroy plans** are unchanged: ModifyPlan returns early and the planned
  state stays null.

## 7. Algorithm

The decisions live in `internal/accumulate`, one file, staying pure Go
with the clock injected as a parameter:

```
// FilterExpired drops values whose stored stamp is not strictly after now
// (X1). Values without a stamp are never dropped. Read calls this with the
// refresh clock; the Update fallback calls it with the apply clock.
FilterExpired(values []string, stamps map[string]time.Time,
    now time.Time) ([]string, map[string]time.Time)

// ClassifyStamps partitions the stampable survivors (result values not in
// planInputs) into keep — a prior stamp that survives unchanged, because
// expiresAfter exists and did not change (S2) — and fresh — everything
// else: no prior stamp, expiresAfter newly set, or expiresAfter changed
// (S1, S3, S4, S5). Values in planInputs belong to neither. It reads no
// clock: ModifyPlan uses it to decide which detailed_outputs entries are
// known and which are unknown until the apply stamps them.
ClassifyStamps(survivors, planInputs []string,
    prior map[string]time.Time,
    priorExpiresAfter, plannedExpiresAfter *int64,
) (keep map[string]time.Time, fresh map[string]struct{})

// FreshStamp computes one fresh value's stamp at apply time: the candidate
// now + plannedExpiresAfter, kept as min/max against the prior stamp when
// expiresAfter changed (S1, S3, S4, S5).
FreshStamp(prior map[string]time.Time,
    priorExpiresAfter, plannedExpiresAfter *int64,
    now time.Time, value string) time.Time

// ApplyExpiration is the fallback-path composition: classify, stamp fresh,
// then filter expired, all with the one clock the fallback owns. It exists
// so the plan-unknown path gets the same decisions as Read plus Update in
// one call. The known paths use the pieces above instead.
ApplyExpiration(result, planInputs []string,
    prior map[string]time.Time,
    priorExpiresAfter, plannedExpiresAfter *int64,
    now time.Time) ([]string, map[string]time.Time)
```

Order of operations, per phase:

1. **Refresh (Read):** parse state, `FilterExpired` with the refresh clock,
   write filtered state back only when something was dropped.
2. **Plan (ModifyPlan, known path):** compute the accumulated result with
   the existing functions (`NextListOutputs` / `NextSetOutputs`), including
   trim — no expiry filtering here, refresh already did it. `ClassifyStamps`
   decides each `detailed_outputs` entry: null for in-`inputs` values, the
   prior stamp for keepers, unknown for fresh entries.
3. **Apply (Update, known path):** reproduce membership and keepers through
   the same pure functions, then resolve each fresh entry with
   `FreshStamp` and the apply clock. Create never stamps (T4). The Update
   fallback uses `ApplyExpiration` with the apply clock.

Resource-specific notes:

- **`accumulator_list`.** Filtering preserves order. Duplicate values share
  one `detailed_outputs` entry (the map is keyed by value); when that value
  leaves `inputs`, all its occurrences expire together, and any re-append
  while it is in `inputs` leaves it null-stamped.
- **`accumulator_set`.** Values are unique, so the map is a natural fit.
- **Create and reseed** (T4) reduce to "everything is in `inputs`", so the
  stamp map is empty and the clock never participates.
- **RFC 3339 handling** lives at the provider boundary: `time.Time` inside
  `internal/accumulate`, strings only in models and state. Writing uses UTC
  second precision; reading accepts any RFC 3339 offset and normalizes.

## 8. Import

Unchanged contract, deliberately deferred:

- The import ID format, `ParseImport`, and every parsing rule are untouched.
  No `expires_at` can be supplied by import.
- Imported state seeds `detailed_outputs` with one all-null entry per seeded
  `outputs` value, so a matching configuration still produces an empty first
  plan.
- The first apply with `expires_after` set in configuration stamps
  non-inputs values through S5, exactly as if `expires_after` had been
  enabled on an old resource.

## 9. State compatibility

Adding two attributes is additive. Prior state that lacks them unmarshals
with both null; `detailed_outputs` null in state is treated as "all null
stamps" by the next plan, which then writes the concrete map. No
`UpgradeState` implementation is expected. Tolerance of both shapes is
verified where each is reachable: acceptance-level import tests cover state
that carries the new attributes as null, and a unit test unmarshals a raw
state object that lacks the new attributes entirely into the resource
model, asserting both land null without error.

## 10. Testing

### 10.1 Unit tests (`internal/accumulate`)

Table-driven tests for all four functions:

- `FilterExpired`: drop at `now == expires_at` (boundary inclusive); keep
  future stamps; never drop unstamped values; nil stamp map; empty input.
- `ClassifyStamps`: keeper vs fresh for every rule shape (S1, S2, S3/S4
  change, S5 no-prior); in-inputs values and null-`expires_after` values in
  neither partition; nil prior map.
- `FreshStamp`: each arm of the min/max switch — decrease keeps the earlier
  bound, increase the later, unchanged keeps the prior stamp — including the
  case where the prior stamp wins over the candidate.
- `ApplyExpiration` (the fallback composition): leaver stamped `now + E`;
  in-inputs value null and never removed; E removed yields no stamps and no
  removals; E = 0 removes a leaver at the same call; duplicates removed
  together; nil prior map; empty result yields empty survivors and empty
  stamps; order preserved; and the increase-rescue arm (S4 plus X1) as
  function-level behavior — reachable in practice only on the fallback path
  with `-refresh=false`, since the primary flow culls expired values at
  refresh before any plan could apply S4.

### 10.2 Acceptance tests (`internal/provider`)

| Test | What it asserts |
| --- | --- |
| list/set create with `expires_after` | outputs seeded; `detailed_outputs` all null |
| leaver stamped on inputs change | value absent from new `inputs` resolves to a future `expires_at` at apply; its entry is unknown in that plan |
| expiry realized on bare refresh | no config change; the step's refresh culls the value; the plan is empty; state no longer contains it |
| in-inputs value survives past its would-be window | never stamped, never removed |
| `expires_after` set on existing resource | history not in `inputs` stamped full window (S5) |
| `expires_after` removed | nothing further expires; remaining `expires_at` null again |
| `expires_after` decreased / increased | min / max behavior observable in `detailed_outputs`; an increase on a still-live value keeps it and restamps it |
| `expires_after` above the upper bound | rejected at plan by the validator |
| E = 0 | value stamped "now" at the apply it leaves `inputs`, culled by the next refresh |
| reset / replacement with `expires_after` set | `detailed_outputs` all null again (T4) |
| re-supply rescues via union | T5: a culled value re-added to `inputs` returns with a null stamp |
| plan-time outputs known | `outputs` fully known at plan on the known path; `detailed_outputs` known except entries this apply stamps, and the test asserts those entries unknown |
| unknown `expires_after` at plan | `detailed_outputs` unknown at plan, resolved at apply |
| steady state with `expires_after` set | second plan after apply is empty |
| increase does not resurrect a culled value | X1 revision: a value expired at refresh is gone; raising `expires_after` cannot bring it back |
| list duplicates expire together | all occurrences culled in one refresh |
| import unchanged | seeded state all-null; first apply with `expires_after` stamps via S5 |

The saved-plan late-apply behavior (section 6) is covered by unit tests of
`ApplyExpiration` (the plan clock is just a parameter) rather than by the
acceptance harness, which does not drive `plan -out` workflows.

### 10.3 House tests

`TestProviderSchema` extends to the new attributes (presence, type, and
Optional/Computed flags on both resources); the em-dash and SPDX rules
apply to the new code unchanged.

## 11. Documentation

- `make docs` regenerates `docs/resources/list.md` and `set.md` from the new
  schema; CI's dirty-tree check enforces it.
- The `expires_after` and `detailed_outputs` attribute descriptions, and the
  resource-level descriptions, state the clock rule in plain language:
  expirations are realized at refresh, when a plan/apply runs after the
  time has passed; entries being stamped by an apply show as known after
  apply in that apply's plan; a late-applied saved plan will not expire an
  item whose time passed after the plan was created — a new plan/apply is
  required; `-refresh=false` defers culling.
- `examples/resources/*/resource.tf` show `expires_after` and a
  `detailed_outputs` reference.

## 12. Decisions and rejected alternatives

- **In-inputs values are null, not stamped.** Chosen by the project owner
  mid-design, and it simplified everything downstream: a value in `inputs`
  cannot expire, so there is nothing to restamp while it is supplied, and no
  apply-time timestamp exists on the steady-state path. An earlier proposal
  restamped in-inputs values every apply ("expires_at = now + expires_after,
  always"), which forced a plan diff on every run and broke the empty-plan
  invariant; it was withdrawn in favor of nulls.
- **Plan-time clock, not apply-time — tried and rejected on 2026-09-24.**
  An apply-time-only design was rejected first because it forces unknown
  values into every plan. The plan-time-clock design (ModifyPlan reading
  `time.Now()` and writing fully-known timestamps) was then implemented and
  rejected on platform evidence: OpenTofu plans every apply twice (saved
  plan, then a re-derivation during apply) and requires known planned
  values to be reproduced identically, so wall-clock values written by
  ModifyPlan fail intermittently with "Provider produced inconsistent final
  plan" whenever the two invocations straddle a second boundary. Reproduced
  with plain `tofu plan -out` plus a delayed `tofu apply`.
- **Refresh-time culling, following terraform-provider-time prior art.**
  Chosen by the project owner after reviewing
  `hashicorp/terraform-provider-time`'s `time_rotating` resource: the clock
  is read only in Read (which culls expired values, the per-value analogue
  of that resource's `RemoveResource`) and in the apply that stamps a fresh
  entry (unknown at plan, like `time_rotating`'s create-time timestamp).
  ModifyPlan stays clock-free, so known planned values are pure functions
  of state and configuration. Accepted consequences: no resurrection by an
  `expires_after` increase, and culling happens even in the run that
  removes `expires_after` (Read cannot see configuration).
- **min(old, candidate) on decrease, max(old, candidate) on increase.**
  Chosen by the project owner. Decreases can only pull expiries earlier,
  increases only later — for values that are still alive when the change
  applies.
- **Culling on stored stamps alone.** Read filters on `expires_at <= now`
  and nothing else, because Read cannot see configuration. This is what
  makes in-`inputs` values safe (they have no stamp) and what removes the
  old rescue behavior as a forced consequence rather than a choice.
- **Trim first, expire second.** Expiring before trim could resurrect values
  beyond the `length` window; filtering after trim cannot.
- **`detailed_outputs` keyed by value, duplicates share an entry.** A map
  cannot represent per-occurrence attributes, and per-occurrence expiry of
  identical strings is indistinguishable in a keyed output anyway. All
  occurrences of a value expire together.
- **Import deferred.** The project owner deferred `expires_at` in import to
  keep the import contract stable; the null-seed path exercises the same S5
  transition as enabling `expires_after` on an old resource.

## 13. Limitations

- Expiration is lazy. A value whose time has passed stays in state until
  some refresh runs; there is no scheduler. `-refresh=false` defers culling
  until the next refresh that does run.
- A saved plan applied late does not expire anything its own refresh did
  not already cull (section 6). Interactive and CI applies refresh, plan,
  and apply back to back and are unaffected.
- `detailed_outputs` cannot distinguish duplicate values in
  `accumulator_list`; they share one entry and expire together.
- The clock is the machine running `terraform plan`. No cross-machine
  synchronization is attempted; stamps are plain wall-clock arithmetic.
