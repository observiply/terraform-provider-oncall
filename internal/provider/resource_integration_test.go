package provider_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/observiply/terraform-provider-oncall/internal/provider"
)

// protoV6ProviderFactories wires the in-process provider binary into
// terraform-plugin-testing's acceptance test runner (TF_ACC=1), which drives
// a real terraform/tofu binary against it.
var protoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"oncall": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// testAccPreCheck fails fast with a clear message if the acceptance
// environment (a live oncall instance + a real API token) isn't configured,
// rather than letting Configure's auth probe produce a confusing error deep
// into the first step.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, e := range []string{"ONCALL_ENDPOINT", "ONCALL_TOKEN"} {
		if os.Getenv(e) == "" {
			t.Fatalf("%s must be set for acceptance tests (see AGENTS.md: task db-up && task migrate-up && task seed-demo && task dev)", e)
		}
	}
}

// TestAccIntegrationResource_SecretWriteOnly is the perpetual-diff and
// PII/secret guards from tfprovider-09-testing.md, scoped to the write-only
// secret attributes added in tfprovider-08: apply with a secret set, then
// assert (a) the plan is empty on a second apply with no config change, and
// (b) the secret value the config sent is nowhere in the resulting state —
// crude, but it's exactly the regression a future contributor accidentally
// widening secret_wo into a normal Computed attribute would trip.
func TestAccIntegrationResource_SecretWriteOnly(t *testing.T) {
	resourceAddr := "oncall_integration.test"
	const secret = "acc-test-secret-value-do-not-persist"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccIntegrationSecretConfig(secret, 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceAddr, "has_secret", "true"),
					resource.TestCheckResourceAttr(resourceAddr, "secret_wo_version", "1"),
					resource.TestCheckNoResourceAttr(resourceAddr, "secret_wo"),
					testAccCheckStateHasNoSecretString(resourceAddr, secret),
				),
			},
			{
				// Same secret_wo_version: the provider must not re-send
				// secret_wo (it has nothing to diff it against), and the
				// plan must come back empty — the perpetual-diff guard.
				Config:   testAccIntegrationSecretConfig(secret, 1),
				PlanOnly: true,
			},
			{
				ResourceName:            resourceAddr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret_wo", "secret_wo_version"},
			},
		},
	})
}

// TestAccIntegrationResource_CRUD covers tfprovider-09-testing.md's four
// minimum acceptance-test criteria for oncall_integration in general
// (create, update in place, import-verify, empty plan) — as opposed to
// TestAccIntegrationResource_SecretWriteOnly above, which is scoped
// narrowly to the write-only secret_wo/secret_wo_version path. This test
// never sets secret_wo, so has_secret stays false throughout and
// ImportStateVerify needs no ignores.
func TestAccIntegrationResource_CRUD(t *testing.T) {
	resourceAddr := "oncall_integration.crud_test"
	var integrationID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy:             testAccCheckIntegrationDestroyed(t, &integrationID),
		Steps: []resource.TestStep{
			{
				Config: testAccIntegrationCRUDConfig("tfacc-integration", "initial description", "https://example.com/hook", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDCaptured(resourceAddr, &integrationID),
					resource.TestCheckResourceAttr(resourceAddr, "name", "tfacc-integration"),
					resource.TestCheckResourceAttr(resourceAddr, "description", "initial description"),
					resource.TestCheckResourceAttr(resourceAddr, "kind", "outgoing_webhook"),
					resource.TestCheckResourceAttr(resourceAddr, "enabled", "true"),
					resource.TestCheckResourceAttr(resourceAddr, "url", "https://example.com/hook"),
					resource.TestCheckResourceAttr(resourceAddr, "http_method", "POST"),
					resource.TestCheckResourceAttr(resourceAddr, "has_secret", "false"),
					resource.TestCheckResourceAttrPair(resourceAddr, "owner_team_id", "data.oncall_team.demo_platform", "id"),
				),
			},
			{
				// Update in place: description/url/enabled change;
				// owner_team_id (RequiresReplace) does not, so id is
				// unchanged.
				Config: testAccIntegrationCRUDConfig("tfacc-integration", "updated description", "https://example.com/hook-v2", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckIDUnchanged(resourceAddr, &integrationID),
					resource.TestCheckResourceAttr(resourceAddr, "description", "updated description"),
					resource.TestCheckResourceAttr(resourceAddr, "url", "https://example.com/hook-v2"),
					resource.TestCheckResourceAttr(resourceAddr, "enabled", "false"),
				),
			},
			{
				// Perpetual-diff guard.
				Config:   testAccIntegrationCRUDConfig("tfacc-integration", "updated description", "https://example.com/hook-v2", false),
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

func testAccIntegrationCRUDConfig(name, description, url string, enabled bool) string {
	return fmt.Sprintf(`
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

resource "oncall_integration" "crud_test" {
  name          = %q
  description   = %q
  kind          = "outgoing_webhook"
  owner_team_id = data.oncall_team.demo_platform.id
  url           = %q
  enabled       = %t
}
`, name, description, url, enabled)
}

func testAccIntegrationSecretConfig(secret string, version int) string {
	return fmt.Sprintf(`
data "oncall_team" "demo_platform" {
  name = "Demo Platform"
}

resource "oncall_integration" "test" {
  name          = "tfprovider-08-acc-test"
  kind          = "outgoing_webhook"
  owner_team_id = data.oncall_team.demo_platform.id
  url           = "https://example.com/tfprovider-08-acc"
  auth_method   = "bearer"

  secret_wo         = %q
  secret_wo_version = %d
}
`, secret, version)
}

// testAccCheckStateHasNoSecretString asserts that none of the resource's
// persisted attribute values contain the given secret — the state-JSON
// guard tfprovider-09-testing.md calls for, applied against the flattened
// attribute map terraform-plugin-testing exposes rather than the raw state
// file, since that's sufficient to catch the same regression (a write-only
// attribute accidentally made Computed, or the secret leaking into some
// other field).
func testAccCheckStateHasNoSecretString(resourceAddr, secret string) resource.TestCheckFunc {
	return func(s *tfstate.State) error {
		rs, ok := s.RootModule().Resources[resourceAddr]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceAddr)
		}
		for attr, val := range rs.Primary.Attributes {
			if strings.Contains(val, secret) {
				return fmt.Errorf("state attribute %s=%q contains the secret value; it must never be persisted", attr, val)
			}
		}
		return nil
	}
}
