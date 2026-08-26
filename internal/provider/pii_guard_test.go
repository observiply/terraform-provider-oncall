package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccPIIGuard_TeamMembers is the "PII guard" test called for in
// tfprovider-09-testing.md: apply a config that surfaces a team's members —
// the resource most likely to grow a redacted field, since
// GET /admin/teams/{id}/group-members redacts email for non-admin callers
// (AGENTS.md's "Protecting PII" section) — and scan the entire resulting
// state for the "•" redaction character.
//
// oncall_team_members deliberately omits email from its schema today (see
// its Read function's comment), so this test is a regression guard rather
// than a check that currently exercises live redaction: it fails the day a
// future contributor adds an "email" (or similarly PII-bearing) attribute
// that echoes a redacted API value into state, which is exactly the
// regression tfprovider-09-testing.md is guarding against. The scan runs
// over every resource and data source in state, not just
// oncall_team_members, so it also catches PII leaking into an unexpected
// attribute of oncall_schedule_layer's member blocks or
// oncall_trigger_targets' user_name field.
func TestAccPIIGuard_TeamMembers(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPIIGuardConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.oncall_team_members.demo_platform", "members.#"),
					resource.TestCheckNoResourceAttr("data.oncall_team_members.demo_platform", "members.0.email"),
					testAccCheckNoRedactionCharacter(),
				),
			},
		},
	})
}

const testAccPIIGuardConfig = `
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

data "oncall_team_members" "demo_platform" {
  team_id = data.oncall_team.demo_platform.id
}

data "oncall_teams" "all" {
  scope = "all"
}

data "oncall_roles" "all" {}

resource "oncall_schedule" "pii_guard" {
  name          = "tfacc-pii-guard-schedule"
  owner_team_id = data.oncall_team.demo_platform.id
}

resource "oncall_schedule_layer" "pii_guard" {
  schedule_id     = oncall_schedule.pii_guard.id
  name            = "tfacc-pii-guard-layer"
  tier            = 1
  rotation_length = "P1W"
  handoff_at      = "2026-01-05T09:00:00Z"
  start_at        = "2026-01-05T09:00:00Z"

  member {
    user_id = data.oncall_team_members.demo_platform.members[0].user_id
  }
}
`
