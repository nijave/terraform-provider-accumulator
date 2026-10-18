resource "accumulator_map" "last_digest" {
  inputs        = { (var.service) = var.image_digest }
  delayed_for   = 600
  expires_after = 3600
}

output "last_digest" {
  value = accumulator_map.last_digest.outputs
}

output "last_digest_all" {
  value = accumulator_map.last_digest.accumulated
}

output "last_digest_detail" {
  value = accumulator_map.last_digest.detailed_outputs
}
