package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccScheduleResource_CRUD covers tfprovider-09-testing.md's four minimum
// acceptance-test criteria for oncall_schedule: create, update in place,
// import-verify, and an empty plan after apply.
func TestAccScheduleResource_CRUD(t *testing.T) {
	resourceAddr := "oncall_schedule.test"
	var scheduleID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy:             testAccCheckScheduleDestroyed(t, &scheduleID),
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleConfig("tfacc-schedule", "initial description", "UTC"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDCaptured(resourceAddr, &scheduleID),
					resource.TestCheckResourceAttr(resourceAddr, "name", "tfacc-schedule"),
					resource.TestCheckResourceAttr(resourceAddr, "description", "initial description"),
					resource.TestCheckResourceAttr(resourceAddr, "timezone", "UTC"),
					resource.TestCheckResourceAttr(resourceAddr, "visible_to_all_teams", "false"),
					resource.TestCheckResourceAttr(resourceAddr, "team_ids.#", "1"),
					resource.TestCheckTypeSetElemAttrPair(resourceAddr, "team_ids.*", "data.oncall_team.demo_platform", "id"),
					resource.TestCheckResourceAttrPair(resourceAddr, "owner_team_id", "data.oncall_team.demo_platform", "id"),
				),
			},
			{
				// Update in place: name/description/timezone all change but
				// owner_team_id (RequiresReplace) does not, so the object
				// must keep the same id.
				Config: testAccScheduleConfig("tfacc-schedule-renamed", "updated description", "America/New_York"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDUnchanged(resourceAddr, &scheduleID),
					resource.TestCheckResourceAttr(resourceAddr, "name", "tfacc-schedule-renamed"),
					resource.TestCheckResourceAttr(resourceAddr, "description", "updated description"),
					resource.TestCheckResourceAttr(resourceAddr, "timezone", "America/New_York"),
				),
			},
			{
				// Perpetual-diff guard: same config, plan must be empty.
				Config:   testAccScheduleConfig("tfacc-schedule-renamed", "updated description", "America/New_York"),
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

func testAccScheduleConfig(name, description, timezone string) string {
	return fmt.Sprintf(`
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

resource "oncall_schedule" "test" {
  name          = %q
  description   = %q
  timezone      = %q
  owner_team_id = data.oncall_team.demo_platform.id
}
`, name, description, timezone)
}
