# terraform-provider-accumulator

Terraform/OpenTofu resources that accumulate a value over successive applies,
with the history stored in state. A list keeps a fixed history length; the other
resources retain every value until a reset, a replacement, or expiry. There is
no endpoint, no credential, no database, and no object store: the provider is
three resources and a small amount of pure list, set, and map logic.

Terraform resources model a desired end state, not a history. When a value needs
to accumulate over successive applies and the history must be readable by
`tofu output` without standing up an external system, these resources are the
declarative equivalent of an LRU.

## Example

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs        = [var.deploy_sha]
  length        = 5
  expires_after = 86400
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}
```

## Resources

- **`accumulator_list`** keeps the most recent `length` inputs. Each apply whose
  `inputs` differs from the previous apply appends the whole new list; a
  `length` change re-trims the existing history. `inputs = []` never erases
  history.
- **`accumulator_set`** remembers every value it has seen, each once, until a
  `triggers_reset` change, a replacement, or (with `expires_after` set)
  expiry forgets it. `outputs` is a set, so Terraform does not preserve or
  promise any element ordering.
- **`accumulator_map`** remembers every key it has seen with the most recent
  value supplied for it. Re-supplying a key with a different value overwrites
  the stored value in place; the key keeps its identity, so it is never
  delayed or expired by a value change. Keys are forgotten by a
  `triggers_reset` change, a replacement, or (with `expires_after` set)
  expiry.

## Expiration

All three resources accept an optional `expires_after` (seconds). A value (or
map key) that leaves `inputs` is removed once `expires_after` seconds have
passed; a value that stays in `inputs` never expires. The computed
`detailed_outputs` map reports each entry's `expires_at` (RFC 3339) or null.
Expiration is lazy and realized at refresh: a value is removed when a refresh
runs after its time has passed, a late-applied saved plan expires nothing its
own refresh did not already remove, and `-refresh=false` defers removal. See
the resource
documentation for the full clock rules.

All three resources also accept an optional `delayed_for` (seconds). A newly
supplied value is recorded in the computed `accumulated` attribute at once
but joins `outputs` only after `delayed_for` seconds have passed, at the
first refresh after that moment. With `delayed_for` null, `accumulated`
and `outputs` are always equal. `delayed_for` must be strictly less than
`expires_after` when both are set. Each `detailed_outputs` entry carries
`visible_at` beside `expires_at`.

All three resources carry `triggers_reset` (discard history and reseed from
`inputs`) and `triggers_replacement` (force a replacement that reseeds). All
three are importable with a JSON seed, since there is no external system to
discover them from:

```sh
tofu import accumulator_list.recent_deploys '{"inputs":["a","b"],"outputs":["a","b","c"]}'
tofu import accumulator_set.seen_hosts '{"outputs":["a","b"]}'
tofu import accumulator_map.last_digest '{"outputs":{"api":"sha256:ab","web":"sha256:cd"}}'
```

Import seeds state only. For `accumulator_list`, `length` is required
configuration and is not seeded, so the first plan after import supplies it from
configuration and the apply re-trims the seeded `outputs`. For
`accumulator_set`, `inputs` is seeded as an empty list; the first plan after
import supplies the configured `inputs` and the apply unions them into the
seeded `outputs`.

For `accumulator_map`, the `outputs` value in the import ID is a JSON object
of strings; `inputs` is seeded as an empty map and the first apply merges the
configured `inputs` into the seeded `outputs`.

## History lives in state

`tofu state rm` (or `terraform state rm`), moving a resource between workspaces
or state files, and state loss all discard the accumulated history. `inputs` is
stored in state, so `tofu plan` and `tofu state show` expose it: do not
accumulate secrets.

## Documentation

The [provider documentation](docs/index.md) lists all three resources. The
provider has no configuration block.

## Requirements

- OpenTofu >= 1.10 (primary target, what CI tests) or Terraform >= 1.10
  (expected to work, not tested)
- Go >= 1.25.12 to build (the module's `go` directive is the floor)

## Development

```sh
make test      # unit tests
make testacc   # acceptance tests; requires tofu >= 1.10 on PATH
make docs      # regenerate docs/ ; requires tofu >= 1.11
```

## License

MPL-2.0. See [LICENSE](LICENSE).
