resource "oncall_trigger" "api_alerts" {
  name          = "API alerts"
  description   = "Ingest endpoint for the API service's alertmanager"
  owner_team_id = data.oncall_team.platform.id
  auth_method   = "bearer" # immutable after creation; the API has no update field for it

  payload_template = <<-EOT
    {
      "title": {{ .labels.alertname | toJSON }},
      "status": {{ .status | toJSON }}
    }
  EOT
}

output "api_alerts_ingest_url" {
  value = oncall_trigger.api_alerts.ingest_url
}

output "api_alerts_token" {
  value     = oncall_trigger.api_alerts.token
  sensitive = true
}
