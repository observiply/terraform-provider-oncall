package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccTriggerTargetsResource_CRUD covers tfprovider-09-testing.md's four
// minimum acceptance-test criteria for oncall_trigger_targets: create,
// update in place, import-verify, and an empty plan after apply. Target
// blocks are listed grouped fired/state_change/webhook throughout, matching
// what GET /admin/triggers/{id}/targets returns, per the resource's own doc
// comment about avoiding a reorder diff.
func TestAccTriggerTargetsResource_CRUD(t *testing.T) {
	resourceAddr := "oncall_trigger_targets.test"
	var targetsID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTriggerTargetsConfigTwo,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDCaptured(resourceAddr, &targetsID),
					resource.TestCheckResourceAttr(resourceAddr, "target.#", "2"),
					resource.TestCheckResourceAttr(resourceAddr, "target.0.target_type", "user"),
					resource.TestCheckResourceAttr(resourceAddr, "target.0.on_event", "fired"),
					resource.TestCheckResourceAttrSet(resourceAddr, "target.0.user_name"),
					resource.TestCheckResourceAttr(resourceAddr, "target.1.target_type", "integration"),
					resource.TestCheckResourceAttr(resourceAddr, "target.1.on_event", "webhook"),
				),
			},
			{
				// Update in place: a state_change target is inserted
				// between the existing fired and webhook targets;
				// trigger_id (RequiresReplace) doesn't change, so id is
				// unchanged.
				Config: testAccTriggerTargetsConfigThree,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDUnchanged(resourceAddr, &targetsID),
					resource.TestCheckResourceAttr(resourceAddr, "target.#", "3"),
					resource.TestCheckResourceAttr(resourceAddr, "target.0.on_event", "fired"),
					resource.TestCheckResourceAttr(resourceAddr, "target.1.on_event", "state_change"),
					resource.TestCheckResourceAttr(resourceAddr, "target.2.on_event", "webhook"),
				),
			},
			{
				// Perpetual-diff guard.
				Config:   testAccTriggerTargetsConfigThree,
				PlanOnly: true,
			},
			{
				ResourceName:      resourceAddr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

const triggerTargetsFixturePreamble = `
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

data "oncall_team_members" "demo_platform" {
  team_id = data.oncall_team.demo_platform.id
}

resource "oncall_trigger" "test" {
  name          = "tfacc-trigger-targets-parent"
  owner_team_id = data.oncall_team.demo_platform.id
  auth_method   = "none"
}

resource "oncall_integration" "test" {
  name          = "tfacc-trigger-targets-integration"
  kind          = "outgoing_webhook"
  owner_team_id = data.oncall_team.demo_platform.id
  url           = "https://example.com/tfacc-trigger-targets"
}
`

const testAccTriggerTargetsConfigTwo = triggerTargetsFixturePreamble + `
resource "oncall_trigger_targets" "test" {
  trigger_id = oncall_trigger.test.id

  target {
    target_type = "user"
    on_event    = "fired"
    user_id     = data.oncall_team_members.demo_platform.members[0].user_id
  }

  target {
    target_type    = "integration"
    on_event       = "webhook"
    integration_id = oncall_integration.test.id
  }
}
`

const testAccTriggerTargetsConfigThree = triggerTargetsFixturePreamble + `
resource "oncall_trigger_targets" "test" {
  trigger_id = oncall_trigger.test.id

  target {
    target_type = "user"
    on_event    = "fired"
    user_id     = data.oncall_team_members.demo_platform.members[0].user_id
  }

  target {
    target_type = "user"
    on_event    = "state_change"
    user_id     = data.oncall_team_members.demo_platform.members[0].user_id
  }

  target {
    target_type    = "integration"
    on_event       = "webhook"
    integration_id = oncall_integration.test.id
  }
}
`
