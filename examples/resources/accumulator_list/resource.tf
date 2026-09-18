resource "accumulator_list" "recent_deploys" {
  inputs = [var.deploy_sha]
  length = 5
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}
