resource "oncall_trigger" "api_trigger" {
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

output "api_trigger_ingest_url" {
  value = oncall_trigger.api_trigger.ingest_url
}

output "api_trigger_token" {
  value     = oncall_trigger.api_trigger.token
  sensitive = true
}
