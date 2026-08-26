// Package provider implements the Terraform provider for oncall.
package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

// machineAPIPath is the mount that accepts a bearer oncall_pat_ token, bypassing
// the browser BFF (cmd/oncall/main.go's machineAPIPrefix + the resource API's own
// /api/v1 base path). The provider always talks to this path; users configure only
// the oncall base URL.
const machineAPIPath = "/m2m/api/v1"

// tokenPrefix is internal/apitoken.token.go's prefix constant, duplicated here
// because the provider has no dependency on oncall's Go modules. Keep in sync.
const tokenPrefix = "oncall_pat_"

const (
	envEndpoint = "ONCALL_ENDPOINT"
	envToken    = "ONCALL_TOKEN"
)

// Ensure OncallProvider satisfies the provider.Provider interface.
var _ provider.Provider = &OncallProvider{}

// OncallProvider is the provider implementation.
type OncallProvider struct {
	// version is stamped by main.go from a build-time -ldflags value (see
	// .goreleaser.yml); "dev" identifies unreleased/local builds in the
	// User-Agent header and in oncall's access logs.
	version string
}

// OncallProviderModel maps provider schema data to a Go type.
type OncallProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
}

// OncallProviderData is passed to resources/data sources via Configure's
// resp.ResourceData / resp.DataSourceData.
type OncallProviderData struct {
	Client *client.ClientWithResponses
}

// New returns a provider.Provider factory for providerserver.Serve.
func New(v string) func() provider.Provider {
	return func() provider.Provider {
		return &OncallProvider{version: v}
	}
}

func (p *OncallProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "oncall"
	resp.Version = p.version
}

func (p *OncallProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages oncall schedules, layers, triggers, and integrations. " +
			"Credentials are an oncall-issued API token (oncall_pat_...), not an authentik " +
			"identity of the provider's own.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional: true,
				Description: "Base URL of the oncall deployment, e.g. https://oncall.example.com. " +
					"Do not include /m2m/api/v1 — the provider appends it. May also be set via the " +
					"ONCALL_ENDPOINT environment variable.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "oncall API token (oncall_pat_...). May also be set via the ONCALL_TOKEN environment variable. Prefer the env var over committing a token in .tf files.",
			},
		},
	}
}

func (p *OncallProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg OncallProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := valueOrEnv(cfg.Endpoint, envEndpoint)
	token := valueOrEnv(cfg.Token, envToken)

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing oncall API endpoint",
			fmt.Sprintf("The provider requires a base URL, set the endpoint attribute or the %s environment variable.", envEndpoint),
		)
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing oncall API token",
			fmt.Sprintf("The provider requires an API token, set the token attribute or the %s environment variable.", envToken),
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if !strings.HasPrefix(token, tokenPrefix) {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Malformed oncall API token",
			fmt.Sprintf(
				"Expected a token starting with %q, minted from the oncall UI (Settings -> API tokens). "+
					"This looks like a different kind of credential (a trigger ingest token or an authentik "+
					"token cannot be used here) — mint a new API token instead.",
				tokenPrefix,
			),
		)
		return
	}

	baseURL, diag := normalizeEndpoint(endpoint)
	if diag != "" {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid oncall endpoint", diag)
		return
	}

	httpClient := &http.Client{}
	c, err := client.NewClientWithResponses(
		baseURL,
		client.WithHTTPClient(httpClient),
		client.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("User-Agent", "terraform-provider-oncall/"+p.version)
			return nil
		}),
	)
	if err != nil {
		resp.Diagnostics.AddError("Unable to construct oncall client", err.Error())
		return
	}

	// Auth probe: surface credential problems once, up front, rather than as N
	// confusing per-resource errors on the first apply.
	probe, err := c.GetAdminTeamsWithResponse(ctx, nil)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to reach oncall",
			fmt.Sprintf("GET /admin/teams against %s failed: %s", baseURL, err),
		)
		return
	}

	switch probe.StatusCode() {
	case http.StatusOK:
		// good
	case http.StatusUnauthorized:
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"oncall rejected this token (401)",
			"The token is invalid, expired, or has been revoked. Under stage 2 token "+
				"resolution this can also mean the upstream authentik account behind the "+
				"token was disabled — mint a new token from the oncall UI.",
		)
	case http.StatusForbidden:
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"oncall rejected this token (403)",
			"The token is valid but its owner lacks the team role required for GET "+
				"/admin/teams. Check the token owner's team membership and role in oncall.",
		)
	case http.StatusServiceUnavailable:
		resp.Diagnostics.AddError(
			"oncall is unavailable (503)",
			"oncall could not reach its upstream identity provider (authentik) to resolve "+
				"this token. This is transient — retry rather than re-authenticating.",
		)
	default:
		resp.Diagnostics.AddError(
			"Unexpected response from oncall",
			fmt.Sprintf("GET /admin/teams against %s returned HTTP %d", baseURL, probe.StatusCode()),
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	data := &OncallProviderData{Client: c}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *OncallProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

func (p *OncallProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// valueOrEnv returns the configured attribute value if set, else falls back to
// the named environment variable.
func valueOrEnv(v types.String, envVar string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return os.Getenv(envVar)
}

// normalizeEndpoint accepts a bare oncall base URL, with or without a trailing
// slash, and also tolerates the common mistake of pasting the full machine API
// path (/m2m/api/v1, or /m2m) — it strips either off rather than erroring, then
// appends machineAPIPath once.
func normalizeEndpoint(raw string) (baseURL, errMsg string) {
	if raw == "" {
		return "", "endpoint must not be empty"
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return "", fmt.Sprintf("endpoint %q must include a scheme (http:// or https://)", raw)
	}

	base := strings.TrimRight(raw, "/")
	for _, suffix := range []string{machineAPIPath, "/m2m"} {
		base = strings.TrimSuffix(base, suffix)
	}
	base = strings.TrimRight(base, "/")

	return base + machineAPIPath, ""
}
