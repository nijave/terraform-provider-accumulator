# The import ID is a JSON object; inputs and outputs are optional and default
# to empty. Seeding inputs lets a matching configuration plan cleanly instead
# of appending the configured list a second time. length is not part of the
# import ID: it is required configuration, so the first plan after import
# supplies it and re-trims the seeded outputs.
terraform import accumulator_list.recent_deploys '{"inputs":["a","b"],"outputs":["a","b","c"]}'
