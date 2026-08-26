# Block order is the escalation order: notify the tier-1 oncall first, wait
# 5 minutes, then repeat once more before falling through to whatever policy
# comes next (e.g. a parent tier).
resource "oncall_schedule_notification_policy" "primary" {
  schedule_id = oncall_schedule.primary.id

  step {
    step_type          = "layer"
    tier               = 1
    wait_after_seconds = 0
  }
  step {
    step_type          = "repeat"
    repeat_count       = 2
    wait_after_seconds = 300
  }
}
