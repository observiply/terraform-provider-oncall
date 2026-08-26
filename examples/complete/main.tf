# A realistic setup, not a feature tour: one schedule with two rotation
# layers, a trigger with routed targets, and an integration — all owned by
# Platform and shared read/write into Payments. This is what people copy, so
# it demonstrates the sharing model deliberately: owner_team_id stays with
# the creating team forever (only Platform can change who this is shared
# with — see the RBAC "Sharing model" section in oncall's AGENTS.md), while
# Payments gets full view/write access without ever being able to re-share
# or un-share any of it.

terraform {
  required_version = ">= 1.11" # write-only attributes, see oncall_integration below

  required_providers {
    oncall = {
      source = "observiply/oncall"
    }
  }
}

provider "oncall" {
  endpoint = "https://oncall.example.com" # or ONCALL_ENDPOINT
  token    = var.oncall_token             # or ONCALL_TOKEN
}

variable "oncall_token" {
  type      = string
  sensitive = true
}

variable "webhook_signing_key" {
  type      = string
  sensitive = true
}

# Teams are administrator-managed, not provider-managed (tfprovider-05 §5) —
# look them up, don't create them.
data "oncall_team" "platform" {
  name = "Platform"
}

data "oncall_team" "payments" {
  name = "Payments"
}

data "oncall_team_members" "platform" {
  team_id = data.oncall_team.platform.id
}

# --- Schedule: two rotation layers -----------------------------------------

resource "oncall_schedule" "primary" {
  name          = "Platform Primary Oncall"
  description   = "Primary and secondary rotation for the Platform team"
  timezone      = "America/New_York"
  owner_team_id = data.oncall_team.platform.id

  # owner_team_id (Platform) is always implicitly included even if omitted
  # here — it's listed anyway for clarity.
  team_ids             = [data.oncall_team.platform.id, data.oncall_team.payments.id]
  visible_to_all_teams = false
}

resource "oncall_schedule_layer" "tier1" {
  schedule_id     = oncall_schedule.primary.id
  name            = "Tier 1 — weekly primary"
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
}

resource "oncall_schedule_layer" "tier2" {
  schedule_id     = oncall_schedule.primary.id
  name            = "Tier 2 — weekly secondary"
  tier            = 2
  rotation_length = "P1W"
  start_at        = "2026-01-05T09:00:00Z"
  handoff_at      = "2026-01-05T09:00:00Z"

  member {
    user_id = data.oncall_team_members.platform.members[2].user_id
  }
  member {
    user_id = data.oncall_team_members.platform.members[0].user_id
  }
}

# Escalate to tier 2 after 5 minutes of no ack on tier 1.
resource "oncall_schedule_notification_policy" "primary" {
  schedule_id = oncall_schedule.primary.id

  step {
    step_type          = "layer"
    tier               = 1
    wait_after_seconds = 0
  }
  step {
    step_type          = "layer"
    tier               = 2
    wait_after_seconds = 300
  }
}

# --- Integration: outbound webhook, shared like the schedule ----------------

resource "oncall_integration" "webhook" {
  name          = "Ops webhook"
  kind          = "webhook"
  owner_team_id = data.oncall_team.platform.id
  team_ids      = [data.oncall_team.platform.id, data.oncall_team.payments.id]

  url         = "https://ops.example.com/hooks/oncall"
  http_method = "POST"
  headers     = jsonencode({ "Content-Type" = "application/json" })

  payload_template = jsonencode({
    text = "{{ .title }}"
  })

  # Write-only: never stored in state, never read back from the API. Bump
  # secret_wo_version to rotate; changing secret_wo alone does nothing.
  auth_method       = "bearer"
  secret_wo         = var.webhook_signing_key
  secret_wo_version = 1
}

# --- Trigger, routed to the schedule and the integration -------------------

resource "oncall_trigger" "api_alerts" {
  name          = "API alerts"
  description   = "Ingest endpoint for the API service's alertmanager"
  owner_team_id = data.oncall_team.platform.id
  team_ids      = [data.oncall_team.platform.id, data.oncall_team.payments.id]
  auth_method   = "bearer" # immutable after creation

  payload_template = <<-EOT
    {
      "title": {{ .labels.alertname | toJSON }},
      "status": {{ .status | toJSON }}
    }
  EOT
}

resource "oncall_trigger_targets" "api_alerts" {
  trigger_id = oncall_trigger.api_alerts.id

  # List blocks in fired, then state_change, then webhook order — the API
  # always returns them grouped that way, so matching it here avoids a
  # reorder diff on the first apply.
  target {
    target_type = "schedule"
    on_event    = "fired"
    schedule_id = oncall_schedule.primary.id
  }
  target {
    target_type    = "integration"
    on_event       = "webhook"
    integration_id = oncall_integration.webhook.id
  }
}

output "api_alerts_ingest_url" {
  value = oncall_trigger.api_alerts.ingest_url
}

output "api_alerts_token" {
  value     = oncall_trigger.api_alerts.token
  sensitive = true
}
