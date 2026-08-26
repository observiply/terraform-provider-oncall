data "oncall_team" "platform" {
  name = "Platform"
}

# Equivalently, look up by id:
# data "oncall_team" "platform" {
#   id = "5b1f9e2a-8c3d-4e11-9c2a-7a5b6d1e0f42"
# }

output "platform_team_id" {
  value = data.oncall_team.platform.id
}
