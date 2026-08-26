package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccScheduleLayerResource_CRUD covers tfprovider-09-testing.md's four
// minimum acceptance-test criteria for oncall_schedule_layer: create,
// update in place, import-verify, and an empty plan after apply.
func TestAccScheduleLayerResource_CRUD(t *testing.T) {
	resourceAddr := "oncall_schedule_layer.test"
	var layerID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy:             testAccCheckLayerDestroyed(t, &layerID),
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleLayerConfig("tfacc-layer", "P1W", 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDCaptured(resourceAddr, &layerID),
					resource.TestCheckResourceAttr(resourceAddr, "name", "tfacc-layer"),
					resource.TestCheckResourceAttr(resourceAddr, "tier", "1"),
					resource.TestCheckResourceAttr(resourceAddr, "rotation_length", "P1W"),
					resource.TestCheckResourceAttr(resourceAddr, "member.#", "1"),
					resource.TestCheckResourceAttrSet(resourceAddr, "member.0.user_id"),
					resource.TestCheckResourceAttrSet(resourceAddr, "member.0.user_name"),
					resource.TestCheckResourceAttr(resourceAddr, "restriction.#", "1"),
					resource.TestCheckResourceAttr(resourceAddr, "restriction.0.day_of_week", "1"),
					resource.TestCheckResourceAttr(resourceAddr, "restriction.0.start_time", "09:00"),
					resource.TestCheckResourceAttr(resourceAddr, "restriction.0.end_time", "17:00"),
				),
			},
			{
				// Update in place: name and rotation_length change; tier and
				// schedule_id (RequiresReplace) do not, so the layer keeps
				// the same id.
				Config: testAccScheduleLayerConfig("tfacc-layer-renamed", "P2W", 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDUnchanged(resourceAddr, &layerID),
					resource.TestCheckResourceAttr(resourceAddr, "name", "tfacc-layer-renamed"),
					resource.TestCheckResourceAttr(resourceAddr, "rotation_length", "P2W"),
				),
			},
			{
				// Perpetual-diff guard.
				Config:   testAccScheduleLayerConfig("tfacc-layer-renamed", "P2W", 1),
				PlanOnly: true,
			},
			{
				ResourceName:      resourceAddr,
				ImportState:       true,
				ImportStateVerify: true,
				// schedule_id isn't returned in a form the import Read can
				// re-derive independently of the id lookup itself, but it
				// *is* returned by GET /admin/layers/{id}; nothing to
				// ignore here in practice, this is left explicit in case a
				// future schema change reintroduces write-only fields.
			},
		},
	})
}

func testAccScheduleLayerConfig(name, rotationLength string, tier int) string {
	return fmt.Sprintf(`
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

data "oncall_team_members" "demo_platform" {
  team_id = data.oncall_team.demo_platform.id
}

resource "oncall_schedule" "test" {
  name          = "tfacc-layer-parent-schedule"
  owner_team_id = data.oncall_team.demo_platform.id
}

resource "oncall_schedule_layer" "test" {
  schedule_id     = oncall_schedule.test.id
  name            = %q
  tier            = %d
  rotation_length = %q
  handoff_at      = "2026-01-05T09:00:00Z"
  start_at        = "2026-01-05T09:00:00Z"

  member {
    user_id = data.oncall_team_members.demo_platform.members[0].user_id
  }

  restriction {
    day_of_week = 1
    start_time  = "09:00"
    end_time    = "17:00"
  }
}
`, name, tier, rotationLength)
}
