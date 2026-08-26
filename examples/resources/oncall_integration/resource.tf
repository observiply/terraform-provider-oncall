resource "oncall_integration" "slack" {
  name          = "Slack #incidents"
  kind          = "outgoing_webhook"
  owner_team_id = data.oncall_team.platform.id

  url         = "https://hooks.slack.com/services/T000/B000/XXXX"
  http_method = "POST"
  headers     = jsonencode({ "Content-Type" = "application/json" })

  payload_template = jsonencode({
    text = "{{ .title }}"
  })

  # Outbound auth secret (e.g. a signing key), sent via a write-only
  # attribute — never stored in state and never read back from the API.
  # Bump secret_wo_version to rotate it; changing secret_wo alone does
  # nothing, since the provider has nothing to diff it against.
  auth_method       = "bearer"
  secret_wo         = var.slack_signing_key
  secret_wo_version = 1
}

variable "slack_signing_key" {
  type      = string
  sensitive = true
}
