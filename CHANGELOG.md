# Changelog

All notable changes to this provider are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html) (with the
0.x caveat that minor versions may carry breaking changes until 1.0.0).

## [0.1.0] - 2026-09-03

Initial release. Protocol version 6.0; requires Terraform >= 1.11 for the
write-only integration secret attributes.

### Added

- Resources: `oncall_schedule`, `oncall_schedule_layer`,
  `oncall_schedule_notification_policy`, `oncall_trigger`,
  `oncall_trigger_targets`, `oncall_integration`. All support import.
- Data sources: `oncall_team`, `oncall_teams`, `oncall_team_members`,
  `oncall_roles`, `oncall_resources`.
- Provider configuration via `endpoint` / `token` attributes or the
  `ONCALL_ENDPOINT` / `ONCALL_TOKEN` environment variables, with an up-front
  token auth probe against `GET /admin/teams`.
- Provider configuration also probes `GET /version` on the oncall deployment
  before anything else. A version request that 404s (oncall too old to expose
  the endpoint) or reports an `api_version` other than the one this provider
  targets fails `terraform plan`/`apply` up front with an actionable
  diagnostic instead of surfacing as confusing per-resource errors on the
  first apply. The probe is unauthenticated, so it doubles as the initial
  reachability check.
- `oncall_integration` outbound auth uses write-only attributes
  (`secret_wo` / `secret_wo_version`) so the secret never lands in state.
- `rotation_length` accepts an ISO 8601 duration string and is kept verbatim
  in state when equivalent to the API's object representation, avoiding a
  perpetual diff (`P1W` vs `P7D`).

[0.1.0]: https://github.com/observiply/terraform-provider-oncall/releases/tag/v0.1.0
