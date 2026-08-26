package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccTriggerResource_CRUD covers tfprovider-09-testing.md's four minimum
// acceptance-test criteria for oncall_trigger: create, update in place,
// import-verify, and an empty plan after apply. auth_method is left at its
// "none" default throughout: auth_method has a RequiresReplace plan
// modifier (so changing it wouldn't be an "update in place" test anyway),
// and with no auth, the API's token field is null both right after create
// and after ImportState's Read, so there's nothing to add to
// ImportStateVerifyIgnore.
func TestAccTriggerResource_CRUD(t *testing.T) {
	resourceAddr := "oncall_trigger.test"
	var triggerID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy:             testAccCheckTriggerDestroyed(t, &triggerID),
		Steps: []resource.TestStep{
			{
				Config: testAccTriggerConfig("tfacc-trigger", "initial description", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDCaptured(resourceAddr, &triggerID),
					resource.TestCheckResourceAttr(resourceAddr, "name", "tfacc-trigger"),
					resource.TestCheckResourceAttr(resourceAddr, "description", "initial description"),
					resource.TestCheckResourceAttr(resourceAddr, "enabled", "true"),
					resource.TestCheckResourceAttr(resourceAddr, "auth_method", "none"),
					resource.TestCheckResourceAttrSet(resourceAddr, "ingest_url"),
					resource.TestCheckNoResourceAttr(resourceAddr, "token"),
					resource.TestCheckResourceAttr(resourceAddr, "team_ids.#", "1"),
					resource.TestCheckResourceAttrPair(resourceAddr, "owner_team_id", "data.oncall_team.demo_platform", "id"),
				),
			},
			{
				// Update in place: name/description/enabled change;
				// owner_team_id and auth_method (both RequiresReplace) do
				// not, so id is unchanged.
				Config: testAccTriggerConfig("tfacc-trigger-renamed", "updated description", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDUnchanged(resourceAddr, &triggerID),
					resource.TestCheckResourceAttr(resourceAddr, "name", "tfacc-trigger-renamed"),
					resource.TestCheckResourceAttr(resourceAddr, "description", "updated description"),
					resource.TestCheckResourceAttr(resourceAddr, "enabled", "false"),
				),
			},
			{
				// Perpetual-diff guard.
				Config:   testAccTriggerConfig("tfacc-trigger-renamed", "updated description", false),
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

func testAccTriggerConfig(name, description string, enabled bool) string {
	return fmt.Sprintf(`
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

resource "oncall_trigger" "test" {
  name          = %q
  description   = %q
  enabled       = %t
  owner_team_id = data.oncall_team.demo_platform.id
  auth_method   = "none"
}
`, name, description, enabled)
}
