resource "accumulator_set" "seen_hosts" {
  inputs        = [var.hostname]
  expires_after = 3600
}

output "seen_hosts" {
  value = accumulator_set.seen_hosts.outputs
}

output "seen_hosts_detail" {
  value = accumulator_set.seen_hosts.detailed_outputs
}
