# The import ID is a JSON object. inputs and outputs are both optional and
# default to empty. Seeding inputs lets a matching configuration plan cleanly
# instead of appending the configured list a second time.
terraform import accumulator_list.recent_deploys '{"inputs":["a","b"],"outputs":["a","b","c"]}'
