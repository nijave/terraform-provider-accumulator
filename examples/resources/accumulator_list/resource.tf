resource "accumulator_list" "recent_deploys" {
  inputs        = [var.deploy_sha]
  length        = 5
  delayed_for   = 300
  expires_after = 86400
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}

output "recent_deploys_all" {
  value = accumulator_list.recent_deploys.accumulated
}

output "recent_deploys_detail" {
  value = accumulator_list.recent_deploys.detailed_outputs
}