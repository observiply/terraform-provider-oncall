data "oncall_team_members" "platform" {
  team_id = data.oncall_team.platform.id
}

# user_id/user_name only — email is redacted by the API for non-admins and
# is never surfaced here.
output "platform_member_ids" {
  value = [for m in data.oncall_team_members.platform.members : m.user_id]
}
