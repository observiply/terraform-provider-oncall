data "oncall_team" "platform" {
  name = "Platform"
}

data "oncall_team" "payments" {
  name = "Payments"
}

resource "oncall_schedule" "primary" {
  name          = "Platform Primary Oncall"
  description   = "Primary rotation for the Platform team"
  timezone      = "America/New_York"
  owner_team_id = data.oncall_team.platform.id

  # Shared into Payments as view-only; owner_team_id is always included
  # automatically even if you leave it out here.
  team_ids             = [data.oncall_team.platform.id, data.oncall_team.payments.id]
  visible_to_all_teams = false
}
