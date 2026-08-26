resource "oncall_integration" "slack" {
  name          = "Slack #incidents"
  kind          = "webhook"
  owner_team_id = data.oncall_team.platform.id

  url         = "https://hooks.slack.com/services/T000/B000/XXXX"
  http_method = "POST"
  headers     = jsonencode({ "Content-Type" = "application/json" })

  payload_template = jsonencode({
    text = "{{ .title }}"
  })

  # The integration's outbound auth secret (e.g. a signing key) is set via a
  # write-only attribute added in tfprovider-08, not here.
}
