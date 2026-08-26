package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccScheduleNotificationPolicyResource_CRUD covers
// tfprovider-09-testing.md's four minimum acceptance-test criteria for
// oncall_schedule_notification_policy: create, update in place,
// import-verify, and an empty plan after apply. Step types "static_user" and
// "repeat" are used (rather than "layer") so the config needs no dependent
// oncall_schedule_layer resource.
func TestAccScheduleNotificationPolicyResource_CRUD(t *testing.T) {
	resourceAddr := "oncall_schedule_notification_policy.test"
	var policyID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNotificationPolicyConfig(30),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDCaptured(resourceAddr, &policyID),
					resource.TestCheckResourceAttr(resourceAddr, "step.#", "2"),
					resource.TestCheckResourceAttr(resourceAddr, "step.0.step_type", "static_user"),
					resource.TestCheckResourceAttr(resourceAddr, "step.0.wait_after_seconds", "30"),
					resource.TestCheckResourceAttr(resourceAddr, "step.1.step_type", "repeat"),
					resource.TestCheckResourceAttr(resourceAddr, "step.1.repeat_count", "2"),
				),
			},
			{
				// Update in place: wait_after_seconds changes; schedule_id
				// (RequiresReplace) does not, so id (== schedule_id) is
				// unchanged.
				Config: testAccNotificationPolicyConfig(60),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDUnchanged(resourceAddr, &policyID),
					resource.TestCheckResourceAttr(resourceAddr, "step.0.wait_after_seconds", "60"),
				),
			},
			{
				// Perpetual-diff guard.
				Config:   testAccNotificationPolicyConfig(60),
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

func testAccNotificationPolicyConfig(waitAfterSeconds int) string {
	return fmt.Sprintf(`
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

data "oncall_team_members" "demo_platform" {
  team_id = data.oncall_team.demo_platform.id
}

resource "oncall_schedule" "test" {
  name          = "tfacc-notifpolicy-parent-schedule"
  owner_team_id = data.oncall_team.demo_platform.id
}

resource "oncall_schedule_notification_policy" "test" {
  schedule_id = oncall_schedule.test.id

  step {
    step_type           = "static_user"
    user_id             = data.oncall_team_members.demo_platform.members[0].user_id
    wait_after_seconds  = %d
  }

  step {
    step_type    = "repeat"
    repeat_count = 2
  }
}
`, waitAfterSeconds)
}
