package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ datasource.DataSource              = &teamsDataSource{}
	_ datasource.DataSourceWithConfigure = &teamsDataSource{}
)

func newTeamsDataSource() datasource.DataSource {
	return &teamsDataSource{}
}

type teamsDataSource struct {
	client *client.ClientWithResponses
}

type teamSummaryModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

type teamsDataSourceModel struct {
	Scope types.String       `tfsdk:"scope"`
	Teams []teamSummaryModel `tfsdk:"teams"`
}

func (d *teamsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_teams"
}

func (d *teamsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists oncall teams.",
		Attributes: map[string]schema.Attribute{
			"scope": schema.StringAttribute{
				Optional: true,
				Description: "\"all\" (default) lists every team regardless of membership; " +
					"any other value lists only the token owner's own teams (GET /admin/teams's default).",
			},
			"teams": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true, Description: "Team UUID."},
						"name":        schema.StringAttribute{Computed: true, Description: "Team name."},
						"description": schema.StringAttribute{Computed: true, Description: "Team description."},
					},
				},
			},
		},
	}
}

func (d *teamsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*OncallProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *provider.OncallProviderData, got: %T", req.ProviderData),
		)
		return
	}
	d.client = data.Client
}

func (d *teamsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config teamsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	scope := "all"
	if !config.Scope.IsNull() && config.Scope.ValueString() != "" {
		scope = config.Scope.ValueString()
	}
	var params *client.GetAdminTeamsParams
	if scope != "" {
		params = &client.GetAdminTeamsParams{Scope: &scope}
	}

	listResp, err := d.client.GetAdminTeamsWithResponse(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list teams", err.Error())
		return
	}
	if listResp.StatusCode() != http.StatusOK || listResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to list teams", "GET", "/admin/teams", listResp.StatusCode(), listResp.Body))
		return
	}

	teams := make([]teamSummaryModel, len(*listResp.JSON200))
	for i, t := range *listResp.JSON200 {
		teams[i] = teamSummaryModel{
			ID:          strFromPtr(t.Id),
			Name:        strFromPtr(t.Name),
			Description: strFromPtr(t.Description),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &teamsDataSourceModel{
		Scope: types.StringValue(scope),
		Teams: teams,
	})...)
}
