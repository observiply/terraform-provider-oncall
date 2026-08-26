package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

// machineAPISuffix mirrors provider.go's unexported machineAPIPath constant.
// Duplicated here (rather than exported from the provider package) because
// the test helper needs to build its own client independent of the provider
// under test, to verify server-side state directly (CheckDestroy).
const machineAPISuffix = "/m2m/api/v1"

// newAccClient builds a raw API client from the same ONCALL_ENDPOINT/
// ONCALL_TOKEN environment variables the provider itself reads, for
// CheckDestroy assertions that need to hit the API independently of
// whatever the provider under test did.
func newAccClient(t *testing.T) *client.ClientWithResponses {
	t.Helper()
	endpoint := strings.TrimRight(os.Getenv("ONCALL_ENDPOINT"), "/")
	token := os.Getenv("ONCALL_TOKEN")
	if endpoint == "" || token == "" {
		t.Fatal("ONCALL_ENDPOINT and ONCALL_TOKEN must be set")
	}
	for _, suffix := range []string{machineAPISuffix, "/m2m"} {
		endpoint = strings.TrimSuffix(endpoint, suffix)
	}
	c, err := client.NewClientWithResponses(
		endpoint+machineAPISuffix,
		client.WithHTTPClient(&http.Client{}),
		client.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("unable to construct test client: %s", err)
	}
	return c
}

// testAccCheckIDCaptured returns a TestCheckFunc that copies the resource's
// id attribute into *out — used to assert an "update in place" step kept the
// same underlying object rather than recreating it.
func testAccCheckIDCaptured(resourceAddr string, out *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resourceAddr, "id", func(v string) error {
		*out = v
		return nil
	})
}

// testAccCheckIDUnchanged returns a TestCheckFunc asserting the resource's
// id attribute still equals the value previously captured by
// testAccCheckIDCaptured — the update-in-place guard (a RequiresReplace bug
// would silently recreate the object with a new id).
func testAccCheckIDUnchanged(resourceAddr string, want *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resourceAddr, "id", func(v string) error {
		if v != *want {
			return fmt.Errorf("resource %s was recreated: id changed from %q to %q", resourceAddr, *want, v)
		}
		return nil
	})
}

// testAccCheckScheduleDestroyed asserts GET /admin/schedules/{id} 404s after
// terraform destroy.
func testAccCheckScheduleDestroyed(t *testing.T, id *string) resource.TestCheckFunc {
	return func(_ *tfstate.State) error {
		if *id == "" {
			return nil
		}
		resp, err := newAccClient(t).GetAdminSchedulesIdWithResponse(context.Background(), *id)
		if err != nil {
			return err
		}
		if resp.StatusCode() != http.StatusNotFound {
			return fmt.Errorf("schedule %s still exists after destroy (HTTP %d)", *id, resp.StatusCode())
		}
		return nil
	}
}

// testAccCheckLayerDestroyed asserts GET /admin/layers/{id} 404s after
// terraform destroy.
func testAccCheckLayerDestroyed(t *testing.T, id *string) resource.TestCheckFunc {
	return func(_ *tfstate.State) error {
		if *id == "" {
			return nil
		}
		resp, err := newAccClient(t).GetAdminLayersIdWithResponse(context.Background(), *id)
		if err != nil {
			return err
		}
		if resp.StatusCode() != http.StatusNotFound {
			return fmt.Errorf("layer %s still exists after destroy (HTTP %d)", *id, resp.StatusCode())
		}
		return nil
	}
}

// testAccCheckTriggerDestroyed asserts GET /admin/triggers/{id} 404s after
// terraform destroy.
func testAccCheckTriggerDestroyed(t *testing.T, id *string) resource.TestCheckFunc {
	return func(_ *tfstate.State) error {
		if *id == "" {
			return nil
		}
		resp, err := newAccClient(t).GetAdminTriggersIdWithResponse(context.Background(), *id)
		if err != nil {
			return err
		}
		if resp.StatusCode() != http.StatusNotFound {
			return fmt.Errorf("trigger %s still exists after destroy (HTTP %d)", *id, resp.StatusCode())
		}
		return nil
	}
}

// testAccCheckIntegrationDestroyed asserts GET /admin/integrations/{id}
// 404s after terraform destroy.
func testAccCheckIntegrationDestroyed(t *testing.T, id *string) resource.TestCheckFunc {
	return func(_ *tfstate.State) error {
		if *id == "" {
			return nil
		}
		resp, err := newAccClient(t).GetAdminIntegrationsIdWithResponse(context.Background(), *id)
		if err != nil {
			return err
		}
		if resp.StatusCode() != http.StatusNotFound {
			return fmt.Errorf("integration %s still exists after destroy (HTTP %d)", *id, resp.StatusCode())
		}
		return nil
	}
}

// testAccCheckNoRedactionCharacter is the PII guard from
// tfprovider-09-testing.md: scan every attribute of every resource and data
// source in the final state for the "•" redaction character the oncall API
// uses to mask emails/destinations for non-admin callers
// (AGENTS.md's "Protecting PII" section). None of the provider's current
// schemas surface a redactable field, so this is a regression guard — it
// fails the moment a future contributor adds e.g. an "email" attribute that
// echoes back a redacted API value.
func testAccCheckNoRedactionCharacter() resource.TestCheckFunc {
	return func(s *tfstate.State) error {
		for addr, rs := range s.RootModule().Resources {
			if rs.Primary == nil {
				continue
			}
			for attr, val := range rs.Primary.Attributes {
				if strings.ContainsRune(val, '•') {
					return fmt.Errorf("state attribute %s.%s=%q contains the PII redaction character; "+
						"a redacted API value leaked into provider state", addr, attr, val)
				}
			}
		}
		return nil
	}
}
