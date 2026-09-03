package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
)

// TestServerVersion hits the live oncall /version endpoint the way the provider
// does (BFF root, machineAPIPath stripped off the configured endpoint) and logs
// the running server's version / api_version. Set ONCALL_ENDPOINT to point at a
// non-default server; otherwise it targets the local dev BFF.
func TestServerVersion(t *testing.T) {
	endpoint := os.Getenv("ONCALL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8080"
	}
	base := strings.TrimRight(endpoint, "/")
	for _, s := range []string{machineAPIPath, "/m2m"} {
		base = strings.TrimSuffix(base, s)
	}
	baseURL := strings.TrimRight(base, "/") + machineAPIPath

	resp := &provider.ConfigureResponse{}
	checkOncallVersion(context.Background(), &http.Client{}, baseURL, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("checkOncallVersion reported errors: %v", resp.Diagnostics.Errors())
	}

	versionURL := strings.TrimSuffix(baseURL, machineAPIPath) + "/version"
	httpResp, err := http.Get(versionURL) //nolint:noctx // short-lived test probe
	if err != nil {
		t.Fatalf("GET %s: %s", versionURL, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	var v oncallVersion
	if err := json.NewDecoder(httpResp.Body).Decode(&v); err != nil {
		t.Fatalf("decode %s: %s", versionURL, err)
	}
	t.Logf("oncall server at %s: version=%q api_version=%q", versionURL, v.Version, v.APIVersion)
}
