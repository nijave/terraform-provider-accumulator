# accumulator_map seeds outputs from a JSON object of strings and hashes it
# for id. An inputs key, if present, is accepted and ignored: inputs is
# seeded as an empty map, and the first plan after import supplies it from
# configuration and merges it into the seeded outputs.
terraform import accumulator_map.last_digest '{"outputs":{"api":"sha256:ab","web":"sha256:cd"}}'
