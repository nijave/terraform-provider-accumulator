# accumulator_set seeds outputs (deduplicated) and hashes them for id. An
# inputs key, if present, is accepted and ignored.
terraform import accumulator_set.seen_hosts '{"outputs":["a","b"]}'
