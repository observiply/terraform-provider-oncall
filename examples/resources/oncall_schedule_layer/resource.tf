data "oncall_team_members" "platform" {
  team_id = data.oncall_team.platform.id
}

resource "oncall_schedule_layer" "primary" {
  schedule_id     = oncall_schedule.primary.id
  name            = "Weekly rotation"
  tier            = 1
  rotation_length = "P1W"
  start_at        = "2026-01-05T09:00:00Z"
  handoff_at      = "2026-01-05T09:00:00Z"

  # Block order is the rotation order.
  member {
    user_id = data.oncall_team_members.platform.members[0].user_id
  }
  member {
    user_id = data.oncall_team_members.platform.members[1].user_id
  }

  # No day_of_week means every day.
  restriction {
    start_time = "09:00"
    end_time   = "17:00"
  }
}
