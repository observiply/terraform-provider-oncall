package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ datasource.DataSource              = &teamDataSource{}
	_ datasource.DataSourceWithConfigure = &teamDataSource{}
)

func newTeamDataSource() datasource.DataSource {
	return &teamDataSource{}
}

type teamDataSource struct {
	client *client.ClientWithResponses
}

type teamDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (d *teamDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (d *teamDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up a single oncall team by id or by name. Exactly one of id or name must be set.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Team UUID.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Team name.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Team description.",
			},
		},
	}
}

func (d *teamDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("name"),
		),
	}
}

func (d *teamDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *teamDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config teamDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !config.ID.IsNull() {
		id := config.ID.ValueString()
		getResp, err := d.client.GetAdminTeamsIdWithResponse(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Unable to read team", err.Error())
			return
		}
		if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
			resp.Diagnostics.Append(unexpectedStatus("Unable to read team", "GET", "/admin/teams/"+id, getResp.StatusCode(), getResp.Body))
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &teamDataSourceModel{
			ID:          strFromPtr(getResp.JSON200.Id),
			Name:        strFromPtr(getResp.JSON200.Name),
			Description: strFromPtr(getResp.JSON200.Description),
		})...)
		return
	}

	name := config.Name.ValueString()
	scope := "all"
	listResp, err := d.client.GetAdminTeamsWithResponse(ctx, &client.GetAdminTeamsParams{Scope: &scope})
	if err != nil {
		resp.Diagnostics.AddError("Unable to list teams", err.Error())
		return
	}
	if listResp.StatusCode() != http.StatusOK || listResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to list teams", "GET", "/admin/teams?scope=all", listResp.StatusCode(), listResp.Body))
		return
	}
	for _, t := range *listResp.JSON200 {
		if t.Name != nil && *t.Name == name {
			resp.Diagnostics.Append(resp.State.Set(ctx, &teamDataSourceModel{
				ID:          strFromPtr(t.Id),
				Name:        strFromPtr(t.Name),
				Description: strFromPtr(t.Description),
			})...)
			return
		}
	}
	resp.Diagnostics.AddError("Team not found", fmt.Sprintf("No team named %q was found (searched scope=all).", name))
}
