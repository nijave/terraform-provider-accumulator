# terraform-provider-accumulator

Terraform/OpenTofu resources that accumulate a value over successive applies
while keeping a fixed history length, with the history stored in state. There is
no endpoint, no credential, no database, and no object store: the provider is
two resources and a small amount of pure list and set logic.

Terraform resources model a desired end state, not a history. When a value needs
to accumulate over successive applies and the history must be readable by
`tofu output` without standing up an external system, these resources are the
declarative equivalent of an LRU.

## Example

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs = [var.deploy_sha]
  length = 5
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
- **`accumulator_set`** remembers every value it has ever seen, each once, in
  the order first observed.

Both resources carry `triggers_reset` (discard history and reseed from `inputs`)
and `triggers_replacement` (force a replacement that reseeds). Both are
importable with a JSON seed, since there is no external system to discover them
from:

```sh
tofu import accumulator_list.recent_deploys '{"inputs":["a","b"],"outputs":["a","b","c"]}'
tofu import accumulator_set.seen_hosts '{"outputs":["a","b"]}'
```

## History lives in state

`terraform state rm`, moving a resource between workspaces or state files, and
state loss all discard the accumulated history. `inputs` is stored in state, so
`tofu plan` and `tofu state show` expose it: do not accumulate secrets.

## Documentation

The [provider documentation](docs/index.md) lists both resources. The provider
has no configuration block.

## Requirements

- OpenTofu >= 1.10 (primary target, what CI tests) or Terraform >= 1.10
  (expected to work, not tested)
- Go >= 1.25 to build

## Development

```sh
make test      # unit tests
make testacc   # acceptance tests; requires tofu >= 1.10 on PATH
make docs      # regenerate docs/ ; requires tofu >= 1.11
```

## License

GPL-3.0-or-later. See [LICENSE](LICENSE).
