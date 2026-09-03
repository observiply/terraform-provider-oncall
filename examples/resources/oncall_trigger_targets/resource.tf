# Block order sets each target's position, but the API tracks position
# independently per on_event and always lists targets grouped as fired, then
# state_change, then webhook — so list blocks in that same grouped order to
# avoid a reorder diff after the first apply.
resource "oncall_trigger_targets" "api_trigger" {
  trigger_id = oncall_trigger.api_trigger.id

  target {
    target_type = "schedule"
    on_event    = "fired"
    schedule_id = oncall_schedule.primary.id
  }
  target {
    target_type    = "integration"
    on_event       = "webhook"
    integration_id = oncall_integration.slack.id
  }
}
