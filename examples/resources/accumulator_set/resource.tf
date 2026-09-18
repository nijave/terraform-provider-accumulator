resource "accumulator_set" "seen_hosts" {
  inputs = [var.hostname]
}

output "seen_hosts" {
  value = accumulator_set.seen_hosts.outputs
}
