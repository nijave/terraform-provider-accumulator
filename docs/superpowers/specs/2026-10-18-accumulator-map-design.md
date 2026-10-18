# accumulator_map — design

Date: 2026-10-18
Status: approved

## 1. Motivation

`accumulator_list` and `accumulator_set` accumulate bare string values. Some
consumers need to remember a value *per key*: the last image digest seen for
each service, the most recent commit for each branch, the IP a host last
reported. Today that takes one set resource per key, or a set of encoded
`"key=value"` strings that the consumer has to split apart and that break on
a value containing the separator.

This feature adds a third resource, `accumulator_map`, whose `inputs` and
`outputs` are `map(string)`. It follows every convention of the set
resource: keys accumulate across applies, a `triggers_reset` change, a
replacement, or expiry forgets a key, a newly seen key can wait
out `delayed_for` before it joins `outputs`, and the resource is importable
with a JSON seed.

## 2. Scope

**In scope:** the `accumulator_map` resource, pure-function support in
`internal/accumulate` (merge, hashing, import parsing), map conversion
helpers in `internal/provider`, unit and acceptance tests, regenerated
documentation, examples, and the README and provider-description updates
that count three resources instead of two.

**Out of scope (deferred):**

- Non-string values (`map(any)`, nested objects). Values are strings,
  matching the other two resources' element type.
- A `length` bound or any ordering. A map has neither; this resource
  mirrors the set, not the list.
- Any change to the existing list and set resources beyond the shared
  house tests that list resource names.

## 3. Concepts

- **Key is the identity.** Every membership, expiry, and delay decision is
  made per key. The value attached to a key rides along and never
  participates in a decision.
- **`inputs`** — required `map(string)`. Terraform stores it in state so
  the value round-trips; the plan and `tofu state show` expose it, so do
  not accumulate secrets (unchanged warning).
- **`accumulated`** — computed `map(string)`; every key ever accumulated
  with its most recent value, including keys still waiting out
  `delayed_for`. Always concrete, `{}` never null.
- **`outputs`** — computed `map(string)`; `accumulated` restricted to the
  keys whose `visible_at` is null, values carried verbatim. With
  `delayed_for` null throughout, `outputs` always equals `accumulated`.
- **`detailed_outputs`** — computed map from every key in `accumulated` to
  `{ expires_at, visible_at }`, the same object type the other resources
  use (`detailedObjectType`). Always concrete, `{}` never null.
- **Overwrite in place.** Re-supplying a key with a different value
  replaces the stored value at that apply, in both `accumulated` and (when
  the key is visible) `outputs`. The key keeps its identity, so a visible
  key stays visible and a pending key keeps its `visible_at`.
  Re-supplying a key with the same value changes nothing.
- **The one invariant.** `outputs` equals `accumulated` filtered to the
  keys whose `visible_at` is null. Every phase preserves it, exactly as on
  the set resource.
- **Only refresh and apply read the clock, never plan.** Unchanged.

## 4. Semantics

"Now" means the clock of the phase the rule names: refresh for cull and
reveal, apply for stamping, never the plan.

**Membership rules:**

- **M1 (merge).** The next `accumulated` is the prior `accumulated` with
  every planned `inputs` entry written over it: a key not yet present is
  added, a key already present takes the planned value. Merge is
  idempotent, so Create, Update, and ModifyPlan apply it unconditionally
  with no `inputs` comparison, matching the set resource.
- **M2 (reseed).** Create, a replacement, and a `triggers_reset` change
  discard history and seed `accumulated` from the planned `inputs` alone.
- **M3 (visible subset).** The next `outputs` equals the next
  `accumulated` minus every key that stays pending after this apply. This
  is a pure function of prior state and configuration; ModifyPlan computes
  it without a clock.

**Expiry rules** (the expiry design's S1 through S5 and X1, restated for
keys):

- A key that leaves `inputs` gets `expires_at = now + expires_after` at
  the apply where it leaves; the entry shows as unknown in that apply's
  plan. A key that stays in `inputs` has a null `expires_at` and never
  expires. Changing `expires_after` recalculates every existing stamp as
  min(old, new) on a decrease and max(old, new) on an increase. Read
  removes every key whose `expires_at` is not strictly after the refresh
  clock.
- A re-supplied key that had an `expires_at` loses it, because the key
  is back in `inputs`, with or without a value change.

**Delay rules** (the delayed-for design's D1 through D5, T1 through T6,
restated for keys):

- A key entering `accumulated` gets `visible_at = now + delayed_for` and
  stays out of `outputs`; the entry shows as unknown in that apply's plan.
  Create and reseed delay every seeded key.
- A pending key keeps its stamp while `delayed_for` stays the same;
  changing `delayed_for` recalculates pending stamps as min / max;
  removing `delayed_for` reveals every pending key at that apply.
- A visible key stays visible whatever happens to `delayed_for` or to its
  value.
- Read culls expired keys first, then clears every `visible_at` not
  strictly after the refresh clock.

**Value rules (new, specific to this resource):**

- **V1 (overwrite, visible key).** A visible key re-supplied with a new
  value shows the new value in `accumulated` and `outputs` at that apply.
  Its `detailed_outputs` entry keeps both stamps null, because the key is
  in `inputs`.
- **V2 (overwrite, pending key).** A pending key re-supplied with a new
  value shows the new value in `accumulated`; it stays out of `outputs`
  and keeps its `visible_at`.
- **V3 (value change is never an entry).** Only a key absent from the
  prior `accumulated` enters. A value change never stamps `visible_at`
  and never restarts a delay.

**Configuration check:**

- **C1.** `delayed_for` must be strictly less than `expires_after` when
  both have known non-null values. Configuration validation checks it and
  plan checks it again, through `checkDelayShorterThanExpiry`.

**Worked example** (`delayed_for = 300`, `expires_after = 900`, plan and
apply back to back):

| Plan/apply | `inputs` | `accumulated` after | `outputs` after | `detailed_outputs` after |
| --- | --- | --- | --- | --- |
| t0 = 10:00 | `{a="1", b="1"}` | `{a="1", b="1"}` | `{}` | `a: visible 10:05`, `b: visible 10:05` |
| t1 = 10:02 | `{a="2", c="1"}` | `{a="2", b="1", c="1"}` | `{}` | `a: visible 10:05`; `b: visible 10:05, expires 10:17`; `c: visible 10:07` |
| t2 = 10:06 | `{a="2", c="1"}` | `{a="2", b="1", c="1"}` | `{a="2", b="1"}` | `a: null`; `b: expires 10:17`; `c: visible 10:07` (t2's refresh reveals a and b; plan empty) |
| t3 = 10:08 | `{a="3", c="1"}` | `{a="3", b="1", c="1"}` | `{a="3", b="1", c="1"}` | `a: null`; `b: expires 10:17`; `c: null` (t3's refresh reveals c; the apply overwrites a, V1) |
| t4 = 10:20 | `{a="3", c="1"}` | `{a="3", c="1"}` | `{a="3", c="1"}` | `a: null`; `c: null` (t4's refresh culls b) |

At t1, `a` changes value while pending and keeps its 10:05 stamp (V2). At
t3, `a` changes value while visible and `outputs` shows the new value at
that apply (V1).

## 5. Schema

| Attribute | Type | Required/Optional/Computed | Validators / modifiers |
| --- | --- | --- | --- |
| `inputs` | `map(string)` | Required | none |
| `triggers_reset` | `string` | Optional | none |
| `triggers_replacement` | `string` | Optional | `stringplanmodifier.RequiresReplace()` |
| `expires_after` | `int64` | Optional | `int64validator.Between(0, maxExpiresAfter)` |
| `delayed_for` | `int64` | Optional | `int64validator.Between(0, maxExpiresAfter)` |
| `accumulated` | `map(string)` | Computed | none |
| `outputs` | `map(string)` | Computed | none |
| `detailed_outputs` | `map(object({expires_at = string, visible_at = string}))` | Computed | none |
| `id` | `string` | Computed | `stringplanmodifier.UseStateForUnknown()` |

Notes:

- The resource type name is `accumulator_map`; the Go type is
  `mapResource`, constructor `NewMapResource`, model `mapResourceModel`.
- Attribute descriptions copy the set resource's text with "set" replaced
  by "map", "value" by "key" where the sentence is about identity, and the
  overwrite rule added to `inputs`, `accumulated`, and `outputs`. The
  em-dash house rule applies.
- `accumulated` and `outputs` have no `UseStateForUnknown`, for the same
  reason the set's lack it: ModifyPlan computes them whenever it knows the
  branch inputs.
- `id` is the lowercase hex SHA-256 of the canonical JSON encoding of the
  initial `inputs` map on create and replacement, and of the seeded
  `outputs` map on import. `encoding/json` sorts map keys, so the encoding
  is canonical without extra work.
- The resource implements `resource.ResourceWithImportState`,
  `ResourceWithModifyPlan`, and `ResourceWithValidateConfig`, like the
  set.

## 6. Plan/apply mechanics

The invariant inherited from the other resources, stated once:

> **The plan shows `outputs` and `accumulated` fully known whenever it
> knows the branch inputs.** The branch inputs are `inputs` (every key and
> every value), both triggers, `expires_after`, and `delayed_for`; when
> any of them is unknown, or any mirrored state attribute is unknown,
> ModifyPlan leaves `outputs`, `accumulated`, and `detailed_outputs`
> unknown and the apply resolves them. The only unknown planned values on
> the known path are the stamps of entries this apply will create.

Mechanically, every phase mirrors `resource_set.go`:

- **Read (refresh) culls, then reveals.** Read parses `accumulated` (or
  `outputs` when `accumulated` is null), runs `FilterExpired` then
  `FilterPending` over its sorted keys with the refresh clock, projects
  the surviving and visible key lists back onto the stored values, and
  writes state only when it dropped or revealed a key.
- **ModifyPlan reads no clock.** It computes the next `accumulated` via
  `NextMapOutputs`, then calls `planStamps` with the sorted keys of the
  result and the sorted keys of the prior `accumulated`; `outputs` is the
  returned visible key list projected onto the merged map.
- **Create and Update** take the known path when the plan holds known
  `outputs`, `accumulated`, and `detailed_outputs`: they copy membership
  from the plan verbatim and `applyStamps` resolves fresh stamps with the
  apply clock. Otherwise `applyFallback` computes membership and stamps
  with the apply clock, and the resource projects the returned key lists
  onto the merged map.
- **Delete** is a no-op.
- **ImportState** seeds `accumulated` and `outputs` from the seeded
  `outputs` map, `inputs` as an empty map, `detailed_outputs` with null
  stamps for every key, and `id` from the seeded `outputs`.

## 7. Algorithm

New pure functions in `internal/accumulate`, in a new file `map.go`:

```
// MergeMap returns a copy of outputs with every entry of inputs written
// over it: a new key is added and an existing key takes the input value.
// Neither argument is mutated. A nil or empty result is an empty
// (non-nil) map. It is idempotent, so callers apply it unconditionally.
MergeMap(outputs, inputs map[string]string) map[string]string

// NextMapOutputs returns the accumulated map accumulator_map writes for
// the next apply: reseed seeds from planInputs alone; otherwise
// MergeMap(stateAccumulated, planInputs).
NextMapOutputs(reseed bool, planInputs, stateAccumulated map[string]string) map[string]string

// SortedKeys returns the keys of m in ascending order; an empty or nil m
// yields an empty (non-nil) slice. Every call from the map resource into
// the stamp functions passes this, so plan and apply classify the same
// keys in the same order.
SortedKeys(m map[string]string) []string

// Project returns the entries of m whose keys are in keys, in a new map.
// It turns a key list returned by a stamp function back into the
// map(string) the resource writes. A key absent from m is skipped.
Project(m map[string]string, keys []string) map[string]string
```

`id.go` gains:

```
// HashMapID is HashID for a map: lowercase hex SHA-256 of the canonical
// JSON object encoding. encoding/json sorts the keys, so two maps with
// the same entries hash the same. A nil map hashes "{}".
HashMapID(values map[string]string) string
```

`import.go` gains:

```
// MapImportSeed is the parsed form of an accumulator_map import ID. A
// missing key is an empty map, never nil.
type MapImportSeed struct {
    Inputs  map[string]string
    Outputs map[string]string
}

// ParseMapImport parses {"inputs":{"k":"v"},"outputs":{"k":"v"}}. The
// same key validation and error style as ParseImport: only "inputs" and
// "outputs" are recognized, each must be a JSON object whose values are
// strings, and errors quote the offending key only.
ParseMapImport(id string) (MapImportSeed, error)
```

The stamp functions (`FilterExpired`, `FilterPending`, `ClassifyStamps`,
`ClassifyDelays`, `FreshStamp`, `Visible`, `ApplyExpiration`,
`ApplyDelay`) and the provider compositions (`stampArgs`, `planStamps`,
`applyStamps`, `applyFallback`) stay unchanged: they take key slices and
stamp maps, and the map resource feeds them `SortedKeys` of its maps. `PlanInputs` is the sorted keys of the planned `inputs`;
`PriorAccumulated` is the sorted keys of the prior `accumulated`.

Order of operations per phase is the set resource's, with a projection
step at the end of each: Read, ModifyPlan, Create, and Update all finish
by calling `Project(merged, keys)` for `accumulated` and `outputs`.

## 8. Provider helpers

`model.go` gains map siblings of the existing list and set helpers:

- `stringMapFromMap(ctx, types.Map) (map[string]string, diag.Diagnostics)`:
  null or unknown yields nil with no diagnostic, matching `stringsFromList`.
- `mapFromStringMap(ctx, map[string]string) (types.Map, diag.Diagnostics)`:
  nil becomes an empty map, never null.
- `mapContainsUnknown(types.Map) bool`: the map itself or any element
  unknown, matching `listContainsUnknown`.

`resource_map.go` holds the resource and its `priorAccumulated` method.
`provider.go` registers `NewMapResource` after `NewSetResource`.

## 9. Import

```sh
tofu import accumulator_map.last_digest '{"outputs":{"api":"sha256:ab","web":"sha256:cd"}}'
```

- The parser accepts and ignores an `inputs` key in the ID and seeds
  `inputs` as an empty map, mirroring the set: the first plan after import
  supplies `inputs` from configuration and merges it into the seeded
  `outputs`.
- `id` hashes the seeded `outputs`.
- Every seeded key is visible with null stamps; with `delayed_for` set,
  nothing is pending until the next key enters.
- An ID whose `outputs` is a JSON array (the list and set shape) fails
  with an error naming the expected object shape.

## 10. State compatibility

A new resource: there is no prior state to read. The resource still
implements the `priorAccumulated` fallback to `outputs` when `accumulated`
is null, so the three resources read state the same way and a hand-edited
state missing `accumulated` still refreshes. No `UpgradeState`.

## 11. Testing

### 11.1 Unit tests (`internal/accumulate`)

- `MergeMap`: new key added; existing key overwritten; same value no
  change; nil / empty arguments yield an empty non-nil map; neither
  argument mutated.
- `NextMapOutputs`: reseed ignores state; non-reseed merges.
- `SortedKeys`: ascending order; nil map yields an empty slice.
- `Project`: keeps only listed keys; skips a key absent from the map;
  result does not alias the input.
- `HashMapID`: equal maps hash equal regardless of insertion order; nil
  and empty hash the same; differs from `HashID` of the key list.
- `ParseMapImport`: both keys; outputs only; inputs only; empty object;
  unknown key rejected; array in place of an object rejected; null
  rejected; non-string value rejected; the error quotes only the key.

### 11.2 Acceptance tests (`internal/provider/resource_map_test.go`)

The set resource's table, adapted to maps, plus the value rules:

| Test | What it asserts |
| --- | --- |
| create | `outputs` and `accumulated` known at plan and equal to `inputs`; `detailed_outputs` has null stamps; `id` is `HashMapID(inputs)` |
| merge across applies | second apply with a new key adds it; prior keys retained with their values |
| overwrite on visible key (V1) | new value in `accumulated` and `outputs` at that apply; entry stamps stay null; plan known |
| overwrite on pending key (V2) | new value in `accumulated`; key still absent from `outputs`; `visible_at` unchanged |
| same value re-supplied | empty plan |
| reset and replacement | reseeded from `inputs`; `id` unchanged on reset, recomputed on replacement |
| `expires_after`: leaver stamped, culled on refresh, rescued by re-supply | mirrors the set's expiry tests |
| `delayed_for`: create pending, reveal on refresh, set on existing, removed, decreased / increased, zero | mirrors the set's delay tests |
| `delayed_for >= expires_after` | rejected at validation on `delayed_for` |
| unknown `inputs` value or key at plan | `outputs`, `accumulated`, `detailed_outputs` unknown at plan; the apply resolves them |
| steady state | second plan empty, pending or not |
| import | seeded `outputs` visible; `inputs` empty; first apply merges configuration; array-shaped ID rejected |
| state with null `accumulated` | Read fills it from `outputs`, plan empty |

### 11.3 House tests

`TestProviderSchema` loops over `accumulator_list`, `accumulator_set`,
and `accumulator_map`, expecting `accumulated` as `map(string)` for the
map resource, and adds an `outputs` type check per resource. The em-dash
and SPDX rules apply unchanged. `TestEveryGoFileHasTheSPDXHeader` covers
the new files automatically.

## 12. Documentation

- `make docs` generates `docs/resources/map.md`; CI's dirty-tree check
  enforces it.
- `examples/resources/accumulator_map/resource.tf` shows a map keyed by
  service with `delayed_for` and `expires_after`, and outputs for
  `outputs`, `accumulated`, and `detailed_outputs`.
  `examples/resources/accumulator_map/import.sh` shows the import ID.
- README and `templates/index.md.tmpl` say "three resources", add an
  `accumulator_map` bullet stating the key identity and overwrite rule,
  and add the import example. The provider `MarkdownDescription` is
  reworded to not count resources, so it stays correct.
- Every attribute description states the overwrite rule once and the
  clock rules by reference to the same text the set uses.

## 13. Decisions and rejected alternatives

- **Overwrite in place.** Chosen by the project owner over "first value
  wins" (a frozen value is rarely what a per-key cache wants) and over
  "a value change re-enters the delay" (needs a second stored value per
  key and a second stamp kind; deferred unless a use case appears).
- **Key is the identity.** Values take no part in expiry or delay; a map
  entry is "the same" when its key is. This is what lets every stamp
  function work on key slices without change.
- **Mirror the set, not the list.** No `length`, no order, no duplicates.
- **Sorted keys everywhere.** Stamp functions preserve order and build
  maps keyed by value; feeding them sorted keys keeps plan and apply
  deterministic and keeps diffs stable.
- **No generic refactor of the existing resources.** A keyed abstraction
  over all three resources would rewrite two working resources and their
  tests to save one resource file; rejected.
- **`"key=value"` strings in the set resource** rejected: ambiguous
  separator, wrong output type.
- **Values are strings.** Matching the other resources' element type;
  `map(any)` would need a dynamic attribute and per-type hashing.

## 14. Limitations

- A value change is invisible to expiry and delay; only key presence
  matters.
- Reveal and expiry are lazy, realized at refresh, with the same
  saved-plan and `-refresh=false` caveats as the other resources.
- State holds values in the clear; do not accumulate secrets.
