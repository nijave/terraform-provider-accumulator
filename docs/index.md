# terraform-provider-accumulator

Resources that accumulate a value over successive applies — a list bounded by
a fixed history length, and a set — with an optional per-value TTL
(`expires_after`) for values that stop being supplied, and an optional
warm-up window (`delayed_for`) before a new value appears in `outputs`. The accumulated
history lives in Terraform/OpenTofu state, so there is no endpoint, no
credential, and no external store to stand up: the provider is two resources
and a small amount of pure list and set logic.

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

Each apply that changes `inputs` appends the new value; the list keeps only the
most recent `length` entries.

## Requirements

- OpenTofu >= 1.10 (the primary target, and what CI tests) or Terraform >= 1.10.
  Terraform is expected to work but is not tested.
- Go >= 1.25.12 to build (the module's `go` directive is the floor).

## History lives in state

`tofu state rm` (or `terraform state rm`), moving a resource between workspaces
or state files, and state loss all discard the accumulated history. `inputs` is
stored in state, so `tofu plan` and `tofu state show` expose it: do not
accumulate secrets.

## License

MPL-2.0. Every dependency is MPL-2.0-compatible (MPL-2.0, BSD-2-Clause,
BSD-3-Clause, MIT, or Apache-2.0).


