# Changelog

All notable changes to this provider are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html) (with the
0.x caveat that minor versions may carry breaking changes until 1.0.0).

## [Unreleased]

### Added

- Provider configuration now probes `GET /version` on the oncall deployment
  before doing anything else. A version request that 404s (oncall too old to
  expose the endpoint) or reports an `api_version` other than the one this
  provider targets fails `terraform plan`/`apply` up front with an actionable
  diagnostic instead of surfacing as confusing per-resource errors on the first
  apply. The probe is unauthenticated, so it doubles as the initial
  reachability check.
- Unit tests for the `main` package: provider address / `TypeName` wiring,
  the default `dev` version string, and a schema-validation sweep over every
  registered resource and data source (`ValidateImplementation`, unique
  type names, provider prefix).
- Unit tests for the `rotation_length` interval helpers
  (`intervalFromString` / `intervalToString` / `layerRespToModel`), covering
  the diff-suppression path that keeps an operator's literal ISO 8601 string
  (e.g. `P7D`) in state when it is equivalent to the interval the API returns.

### Changed

- The trigger examples use `api_trigger` as the resource label instead of
  `api_alerts` (documentation only — no resource type or attribute changed).

## [0.1.0] - 2026-09-02

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
- `oncall_integration` outbound auth uses write-only attributes
  (`secret_wo` / `secret_wo_version`) so the secret never lands in state.
- `rotation_length` accepts an ISO 8601 duration string and is kept verbatim
  in state when equivalent to the API's object representation, avoiding a
  perpetual diff (`P1W` vs `P7D`).

[Unreleased]: https://github.com/observiply/terraform-provider-oncall/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/observiply/terraform-provider-oncall/releases/tag/v0.1.0
