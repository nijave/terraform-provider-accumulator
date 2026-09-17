# terraform-provider-accumulator — design

Date: 2026-09-17
Status: proposed (awaiting review)

## 1. Motivation

Terraform resources model a desired end state, not a history. Sometimes that is
the wrong shape: a value needs to accumulate over successive applies while the
configuration keeps a fixed history length, and the history must be readable by
`terraform output` / `tofu output` without standing up a database, object store,
or secrets manager just to hold a list.

The usual workaround is to push the list into an external system (`aws_ssm`,
Consul KV, a Kubernetes ConfigMap) and read it back with a data source. That
works, but it adds an external dependency that has to exist before the first
apply and is easy to delete by accident.

`terraform-provider-accumulator` keeps the history in Terraform state. There is
no endpoint, no credential, and no client. The provider is two resources and a
small amount of pure list/set logic.

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs = [var.deploy_sha]
  length = 5
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}
```

Each apply that changes `inputs` appends the new value; the list keeps only the
most recent `length` entries.

## 2. Scope

**In scope:** the provider binary, the two resources, import, unit and
acceptance tests, generated documentation, and the build/CI/release pipeline.

**Out of scope:**

- Data sources and provider-defined functions. The state attributes are already
  readable directly; there is nothing to query that the resource does not
  expose.
- Any external persistence. State is the only store.
- Provider-level configuration. There is nothing to configure.

**Explicit non-goals:**

- Deduplication in `accumulator_list`. The resource is deliberately naive about
  the contents of `inputs` (see §4).
- Cross-resource or cross-workspace accumulation. Two resource instances have
  two independent histories.
- Surviving `terraform state rm`, workspace moves, or state loss. If state is
  discarded, the accumulated history is discarded with it.

## 3. Stack and repository layout

Go with `terraform-plugin-framework` (protocol 6). SDKv2 is not considered: it
is in maintenance mode, and the framework is what the sibling
`terraform-provider-pki` already uses.

**OpenTofu is the primary target.** The support floor is OpenTofu ≥ 1.10, which
is the oldest release line still receiving security support per
<https://endoflife.date/opentofu> (as of 2026-09-17: 1.10 and 1.12). From 1.11
onwards each release line's security support is aligned to the Go release
cycle, so 1.11 ended with Go 1.25 on 2026-08-19. The code uses no OpenTofu-only
or Terraform-only features, so Terraform ≥ 1.10 works too, but Terraform is not
tested and is not the reference platform: CI drives `tofu` exclusively, which
keeps BUSL-licensed binaries out of the build.

Provider address: `registry.opentofu.org/nijave/accumulator`; short name in
configuration and in the test factories: `accumulator`. Module path:
`github.com/nijave/terraform-provider-accumulator`. Go directive `go 1.25.12`
(matching pki, and above the `go 1.25.8` that terraform-plugin-testing v1.16.0
requires); the build toolchain here is Go 1.26.8.

There is no OpenTofu fork of `terraform-plugin-framework`, `terraform-plugin-go`,
`terraform-plugin-testing`, or `terraform-plugin-docs`, and none is needed.
OpenTofu implements the same protocol and reads the same `docs/` layout. Those
remain MPL-2.0 libraries; see §13.

```
terraform-provider-accumulator/
  main.go                              # providerserver.Serve, ldflags-injected version
  internal/
    accumulate/                        # pure Go, zero Terraform imports
      list.go                          # Trim, Append
      set.go                           # Merge
      id.go                            # HashID
      import.go                        # ParseImport
      boundary_test.go                 # fails `go test` if a terraform-plugin-* import appears
      *_test.go
    provider/
      provider.go                      # provider.New, Metadata, Schema, Resources
      resource_list.go                 # accumulator_list
      resource_set.go                  # accumulator_set
      model.go                         # shared types.List/types.Set <-> []string conversion
      provider_test.go                 # in-process protocol 6 factories, shared guards
      resource_list_test.go
      resource_set_test.go
  docs/
    index.md                           # generated
    resources/accumulator_list.md      # generated
    resources/accumulator_set.md       # generated
    superpowers/specs/                 # this document (hand-written, never clobbered)
  examples/
    provider/provider.tf
    resources/accumulator_list/resource.tf
    resources/accumulator_list/import.sh
    resources/accumulator_set/resource.tf
    resources/accumulator_set/import.sh
  templates/index.md.tmpl              # source for docs/index.md
  tools/                               # separate Go module: tfplugindocs only
    go.mod
    tools.go
    gen-schema.sh
  GNUmakefile
  .goreleaser.yml
  terraform-registry-manifest.json     # {"version": 1, "metadata": {"protocol_versions": ["6.0"]}}
  .github/workflows/test.yml
  .github/workflows/release.yml
  .github/dependabot.yml
  .gitignore
  LICENSE                              # GPL-3.0
  README.md
```

`internal/accumulate` holds the list and set behavior so it can be tested
without the framework and without `TF_ACC`. `internal/provider` is a thin
translation between `types.*` values and the pure functions.

## 4. Concepts and invariants

Two resources, sharing one contract except for how values are combined.

- **`inputs`** is the configuration value. Terraform stores it in state, so the
  provider can compare the planned `inputs` against the previous apply's
  `inputs`.
- **`outputs`** is the accumulated history. It is `Computed`, never configured.
- **A "change to `inputs`"** means the whole planned list differs from the list
  in state. When that happens, the entire new list is treated as new input, even
  if it overlaps the previous list or reuses a value seen several applies ago.
- **Trimming** keeps the last `length` elements. A list of length 0 produces an
  empty output.
- **Reset** discards history and seeds `outputs` from the current `inputs`.
- **Replacement** destroys and recreates the resource, which reseeds it from the
  current `inputs`.

Invariants that acceptance tests assert:

1. A second plan after a successful apply is empty (no attribute recomputes on
   refresh).
2. `inputs = []` never erases history; only `length` (list only),
   `triggers_reset`, or `triggers_replacement` can.
3. `outputs` is always at most `length` elements for `accumulator_list`.
4. `accumulator_set` never contains a value twice, and never forgets a value
   except on a `triggers_reset` change or a replacement.

## 5. Resource schemas

### 5.1 `accumulator_list`

| Attribute | Type | Required/Optional/Computed | Modifiers and validators |
| --- | --- | --- | --- |
| `inputs` | `list(string)` | Required | none |
| `length` | `int64` | Required | `int64validator.AtLeast(0)` |
| `triggers_reset` | `string` | Optional (default null) | none |
| `triggers_replacement` | `string` | Optional (default null) | `stringplanmodifier.RequiresReplace()` |
| `outputs` | `list(string)` | Computed | none |
| `id` | `string` | Computed | `stringplanmodifier.UseStateForUnknown()` |

`outputs` deliberately has no `UseStateForUnknown`: it must plan as unknown
whenever any input changes, because the applied value depends on the prior
state. Copying the old value into the plan would show a stale list and then
fail the apply with an inconsistent-result error.

`inputs` has no plan modifier, so a change plans an in-place update rather than
a replacement.

### 5.2 `accumulator_set`

Same attributes minus `length`:

| Attribute | Type | Required/Optional/Computed | Modifiers and validators |
| --- | --- | --- | --- |
| `inputs` | `list(string)` | Required | none |
| `triggers_reset` | `string` | Optional (default null) | none |
| `triggers_replacement` | `string` | Optional (default null) | `stringplanmodifier.RequiresReplace()` |
| `outputs` | `set(string)` | Computed | none |
| `id` | `string` | Computed | `stringplanmodifier.UseStateForUnknown()` |

`inputs` stays a list, matching the shape users write in configuration and the
shape the spec describes. Order does not affect the result, because the values
are unioned.

### 5.3 Provider

No configuration attributes. `Metadata.TypeName` is `accumulator`; resources are
named `accumulator_list` and `accumulator_set` via
`req.ProviderTypeName + "_list"` / `"_set"`.

## 6. Accumulation algorithm

All list handling normalizes null and unknown to an empty slice before
computing, and always writes a concrete value back (`types.ListValueFrom` /
`types.SetValueFrom`), never null.

### 6.1 `accumulator_list`

**Create** (also runs after a replacement):

```
outputs = Trim(plan.inputs, plan.length)
id      = HashID(plan.inputs)
```

**Update:**

```
if triggers_reset changed:
    outputs = Trim(plan.inputs, plan.length)
else if plan.inputs != state.inputs:
    outputs = Append(state.outputs, plan.inputs, plan.length)
else:                       # only length changed
    outputs = Trim(state.outputs, plan.length)
id = state.id               # unchanged
```

Comparing `triggers_reset` uses `types.String.Equal`, which handles null. Null
to any value, and value to a different value, both count as a change; an
unchanged null does not. `triggers_replacement` is never inspected in Update
because a change to it plans a replacement and Update is not called.

Worked examples, all matching the original specification:

| Step | `inputs` | `length` | `outputs` |
| --- | --- | --- | --- |
| create | `["a"]` | 1 | `["a"]` |
| update | `["b"]` | 1 | `["b"]` |
| create | `["a"]` | 2 | `["a"]` |
| update | `["b"]` | 2 | `["a","b"]` |
| update | `["c"]` | 2 | `["b","c"]` |
| create | `["a","b"]` | 1 | `["b"]` |
| create | `["a"]` | 1 | `["a"]` |
| update | `[]` | 1 | `["a"]` |
| update | `[]` | 0 | `[]` |

An update that changes `inputs` from `["a"]` to `["a","b"]` appends the whole
new list, producing `["a","a","b"]` at sufficient `length`. That is intended:
there is no deduplication.

### 6.2 `accumulator_set`

**Create:** `outputs = Merge(nil, plan.inputs)`, `id = HashID(plan.inputs)`.

**Update:**

```
if triggers_reset changed:
    outputs = Merge(nil, plan.inputs)
else:
    outputs = Merge(state.outputs, plan.inputs)
id = state.id
```

No `inputs` comparison is needed: union is idempotent, so applying it whenever
Update runs is correct, and Update does not run when nothing changed.

### 6.3 Pure functions

```
// Trim returns the last length elements. A length <= 0 yields an empty slice;
// a length >= len(values) yields the whole slice. A nil input yields an empty
// (non-nil) slice so downstream JSON is "[]".
Trim(values []string, length int) []string

// Append returns Trim(outputs ++ inputs, length).
Append(outputs, inputs []string, length int) []string

// Merge returns the union of outputs and inputs, first occurrence wins, order
// stable for deterministic state.
Merge(outputs, inputs []string) []string
```

### 6.4 Read and Delete

Both are structural no-ops, because state is the only store. The framework
copies the prior state into the Read response before calling `Read`, so an
empty method returns it unchanged and refresh can never perturb an attribute;
that is what makes invariant 1 hold. There is nothing external to destroy, and
the framework removes the resource from state automatically when `Delete`
returns without errors, so an empty method is the whole implementation there
too.

## 7. Resource identity

`id` is `HashID(initialInputs)`: the lowercase hex SHA-256 of the canonical
JSON encoding of the `inputs` list present when the resource was created.

- Deterministic: recreating a resource with the same initial `inputs` yields the
  same `id`.
- Stable across in-place updates, because `id` carries `UseStateForUnknown` and
  Update never rewrites it.
- Recomputed on replacement, because Create runs again against the `inputs` at
  that moment. If a replacement is triggered by `triggers_replacement` while
  `inputs` is unchanged, the recomputed value is identical to the previous one.
  That is a consequence of hashing only `inputs`, and it is harmless: `id` is
  informational, and Terraform keys state by resource address, not by `id`.
- On import, see §8: the list resource hashes the seeded `inputs`; the set
  resource, which seeds no meaningful `inputs`, hashes the seeded `outputs`.

`HashID` normalizes nil to an empty slice, so an empty `inputs` hashes `[]`.

## 8. Import

An import ID is a JSON object whose values are arrays of strings. It is the
seeding format for a resource that cannot be discovered from anywhere.

- `accumulator_list`:
  `{"inputs":["a","b"],"outputs":["a","b","c"]}`
- `accumulator_set`:
  `{"outputs":["a","b"]}`

Parsing rules:

- The top level must be a JSON object. A bare array, a scalar, or invalid JSON
  is an error.
- Recognized keys are `inputs` and `outputs`. An unrecognized key is an error,
  so a typo like `outupts` cannot silently import an empty resource.
- Each value must be an array of strings. Numbers, objects, and null are
  errors.
- A missing key means empty. `{}` imports a resource with empty `inputs` and
  empty `outputs`.

Import behavior:

**`accumulator_list`:** seed `inputs` from the `inputs` key, `outputs` from the
`outputs` key, `id = HashID(inputs)`. Seed `length`, `triggers_reset`, and
`triggers_replacement` as null; `length` is required configuration, so the next
plan fills it in and Update re-trims the seeded `outputs`. Seeding `inputs` is
what lets a matching configuration produce an empty plan instead of appending
the configured list a second time.

**`accumulator_set`:** seed `outputs` from the `outputs` key (deduplicated),
`id = HashID(outputs)`. Set `inputs` to an empty list rather than null: it is a
required attribute, and an empty list is a valid non-null value, so nothing
depends on tolerating a null required attribute in imported state. The next plan
sets `inputs` from configuration and Update unions it into the seeded `outputs`.
An `inputs` key in the import ID is accepted and ignored.

Import IDs are not sensitive. They are printed in full by the CLI before the
provider runs, which is acceptable because the format carries no credentials.

## 9. Error handling and diagnostics

- JSON parse failures and schema violations in an import ID produce an
  `AddError` diagnostic naming the expected shape and quoting the offending key
  only, never the full payload.
- `ElementsAs` failures during plan/state conversion produce an `AddError`
  diagnostic.
- Update has no reachable internal error state, so it adds a diagnostic only if
  a conversion fails.

## 10. Testing

### 10.1 Unit tests

`internal/accumulate` carries table-driven tests for `Trim`, `Append`, `Merge`,
`HashID`, and `ParseImport`, including nil and empty inputs, `length` of zero
and negative, duplicate values, and every malformed import shape. A boundary
test in the same package parses its own imports and fails if any file references
a `terraform-plugin-*` package, mirroring `terraform-provider-pki`.

`internal/provider` carries framework-level guards adapted from pki:

- `TestProviderSchema` validates the whole schema with `IsUnitTest: true`,
  which needs no `TF_ACC`.
- `TestEveryGoFileHasTheSPDXHeader` walks the module and requires the license
  header on every `.go` file.
- A house-style test that user-facing strings do not contain `--` where an em
  dash is intended.

### 10.2 Acceptance tests

Harness, copied from pki:

- In-process factories: `providerserver.NewProtocol6WithError(provider.New("test")())`
  registered under `accumulator`.
- `testAccPreCheck` requires `TF_ACC`, `TF_ACC_TERRAFORM_PATH`, and
  `TF_ACC_PROVIDER_HOST=registry.opentofu.org` and fails with an actionable
  message rather than letting the harness download Terraform.
- No version checks are needed; the floor is 1.10 and CI never runs below it.

Cases that apply configuration end with `PostApplyPostRefresh: ExpectEmptyPlan`:

| Test | What it asserts |
| --- | --- |
| list create | `outputs = Trim(inputs, length)`, `id` matches `HashID(inputs)` |
| list append and trim | successive updates produce the documented append-and-trim results |
| list empty inputs | `inputs = []` retains `outputs` |
| list length shrink and grow | outputs re-trim; growing does not resurrect values |
| list length zero | `outputs = []` |
| list unchanged inputs | re-applying the same config is a no-op |
| list reset | changing `triggers_reset` reseeds `outputs` from `inputs` |
| list replacement | changing `triggers_replacement` replaces and reseeds |
| set union | successive updates accumulate, deduplicated |
| set reset | changing `triggers_reset` reseeds |
| set replacement | changing `triggers_replacement` replaces |
| list import | JSON seed yields the stated `inputs`/`outputs`; a following apply is empty |
| set import | JSON seed yields the stated `outputs`; a following apply unions config `inputs` |
| malformed import | bad JSON, unknown key, and wrong value type each fail with a named error |

## 11. Documentation

`docs/` is generated by `tfplugindocs` from the schema and `examples/`, then
checked in. `make docs` runs `tools/gen-schema.sh` followed by
`go generate ./...` in the `tools` module, and CI fails on a resulting
`git diff`.

`tools/gen-schema.sh` is adapted from pki: it builds the provider, exports the
schema with `tofu providers schema -json` through a `dev_overrides` CLI config,
and rewrites the `registry.opentofu.org/hashicorp/accumulator` key to the bare
`accumulator` name that `tfplugindocs --providers-schema` expects. Using
OpenTofu for this step keeps `terraform` out of the toolchain.

The script inherits pki's `OpenTofu >= 1.11` requirement, a build-time floor
above the provider's 1.10 support floor. Schema export output changes when the
exporting CLI crosses a feature threshold, which is the doc drift that pushed
pki off a downloaded Terraform, so CI pins the exact `tofu` version (§12);
local `make docs` works with any `tofu` ≥ 1.11.

`docs/superpowers/` is hand-written and is not an input or output of
generation. The generate job's dirty-tree check would catch an accidental change
to it, and `tools/schema.json` is gitignored so exporting the schema is not
mistaken for drift.

Examples: `examples/provider/provider.tf` (empty provider block), and one
`resource.tf` plus `import.sh` per resource.

## 12. Build, CI, and release

`GNUmakefile` targets: `build`, `test`, `testacc`, `fmt`, `vet`, `docs`,
`release`. `testacc` sets `TF_ACC=1`, `TF_ACC_TERRAFORM_PATH=$(command -v tofu)`,
and `TF_ACC_PROVIDER_HOST=registry.opentofu.org`.

`.github/workflows/test.yml` jobs, on pull requests and pushes to `main`:

- **build** — `go build`, `gofmt -l` must be empty, `go vet`.
- **unit** — `go test -v -cover ./internal/...`.
- **generate** — install a pinned OpenTofu (≥ 1.11, what `tools/gen-schema.sh`
  requires; pki pins `1.12.4` via `opentofu/setup-opentofu@v2`), run
  `make docs`, fail if the tree is dirty.
- **acceptance** — matrix `tofu` `1.10.*` and `1.12.*` (oldest supported line
  and newest), `go test -v -cover ./internal/provider/` with the three `TF_ACC*`
  environment variables set.
- **license** — `go-licenses check` with forbidden/restricted/unknown
  disallowed, so nothing GPL-incompatible can enter the dependency graph.

`.github/workflows/release.yml` runs goreleaser on `v*` tags with GPG signing
and a draft release, matching pki. It requires `GPG_PRIVATE_KEY` and `PASSPHRASE`
repository secrets.

`.goreleaser.yml` builds `CGO_ENABLED=0`, `-trimpath`, for
freebsd/windows/linux/darwin on amd64/arm64, names the binary
`terraform-provider-accumulator_v{{ .Version }}`, signs the checksum file, and
ships `terraform-registry-manifest.json` as an extra file.

`dependabot.yml` covers GitHub Actions, the root Go module (grouping
`github.com/hashicorp/terraform-plugin-*`), and the `tools` module.

## 13. License

GPL-3.0-or-later, matching pki. Every `.go` file begins with
`// SPDX-License-Identifier: GPL-3.0-or-later`. Dependencies must be
GPLv3-compatible; the audited set is MPL-2.0, BSD-3-Clause, MIT, and Apache-2.0.
Nothing under BUSL-1.1 may be linked or downloaded: a provider is a separate
process speaking gRPC, so Terraform CLI is not a dependency, and CI installs
OpenTofu instead.

## 14. Decisions and rejected alternatives

- **Framework over SDKv2.** SDKv2 is in maintenance mode and the sibling
  provider is already on the framework.
- **No deduplication in `accumulator_list`.** Chosen by the project owner.
  Re-submitting a value appends it again; only `accumulator_set` deduplicates.
- **Two triggers, not one.** `triggers_reset` resets history in place;
  `triggers_replacement` forces a replacement. Both resources carry both.
- **No `ModifyPlan`.** `outputs` is left unknown at plan time when it will
  change. Computing the planned value would improve `plan` output but introduces
  a second implementation of §6 that can disagree with Update and produce
  inconsistent-result errors. Rejected for now; it can be added later without a
  state change.
- **Import by JSON seed.** A resource with no external system cannot be
  discovered, so import takes an explicit serialized payload. A plain
  passthrough ID was rejected: it would import an empty resource with no way to
  supply history.
- **State is the only store.** Accepted limitation; see §15.

## 15. Limitations

- History lives in Terraform state. `terraform state rm`, moving a resource
  between workspaces or state files, and state loss all discard it.
- `inputs` is stored in state by Terraform, so `terraform plan` and
  `terraform state show` expose it. Do not accumulate secrets.
- `length` cannot be seeded by import; the first apply after import sets it and
  re-trims `outputs` to it.
