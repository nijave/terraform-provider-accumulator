resource "accumulator_list" "recent_deploys" {
  inputs        = [var.deploy_sha]
  length        = 5
  expires_after = 86400
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}

output "recent_deploys_detail" {
  value = accumulator_list.recent_deploys.detailed_outputs
}
