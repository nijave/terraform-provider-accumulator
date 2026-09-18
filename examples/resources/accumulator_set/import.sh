# accumulator_set seeds outputs (deduplicated) and hashes them for id. An
# inputs key, if present, is accepted and ignored: inputs is seeded as an
# empty list, and the first plan after import supplies it from configuration
# and unions it into the seeded outputs.
terraform import accumulator_set.seen_hosts '{"outputs":["a","b"]}'
