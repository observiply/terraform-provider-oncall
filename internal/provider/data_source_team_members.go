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
	_ datasource.DataSource              = &teamMembersDataSource{}
	_ datasource.DataSourceWithConfigure = &teamMembersDataSource{}
)

func newTeamMembersDataSource() datasource.DataSource {
	return &teamMembersDataSource{}
}

type teamMembersDataSource struct {
	client *client.ClientWithResponses
}

type teamMemberSummaryModel struct {
	UserID   types.String `tfsdk:"user_id"`
	UserName types.String `tfsdk:"user_name"`
}

type teamMembersDataSourceModel struct {
	TeamID  types.String             `tfsdk:"team_id"`
	Members []teamMemberSummaryModel `tfsdk:"members"`
}

func (d *teamMembersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team_members"
}

func (d *teamMembersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up the user ids and display names of a team's members, for " +
			"referencing them in oncall_schedule_layer member blocks. Deliberately excludes " +
			"email: GET /admin/teams/{id}/group-members redacts it for non-admins, so it " +
			"is dropped here rather than letting a redacted value ever enter state.",
		Attributes: map[string]schema.Attribute{
			"team_id": schema.StringAttribute{
				Required:    true,
				Description: "Team UUID.",
			},
			"members": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"user_id":   schema.StringAttribute{Computed: true, Description: "User UUID."},
						"user_name": schema.StringAttribute{Computed: true, Description: "Display name."},
					},
				},
			},
		},
	}
}

func (d *teamMembersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *teamMembersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config teamMembersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	teamID := config.TeamID.ValueString()
	listResp, err := d.client.GetAdminTeamsIdGroupMembersWithResponse(ctx, teamID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list team members", err.Error())
		return
	}
	if listResp.StatusCode() != http.StatusOK || listResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to list team members", "GET", "/admin/teams/"+teamID+"/group-members", listResp.StatusCode(), listResp.Body))
		return
	}

	// Deliberately map only user_id/user_name — never UserEmail, which the
	// API redacts for non-admins.
	members := make([]teamMemberSummaryModel, len(*listResp.JSON200))
	for i, m := range *listResp.JSON200 {
		members[i] = teamMemberSummaryModel{
			UserID:   strFromPtr(m.UserId),
			UserName: strFromPtr(m.UserName),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &teamMembersDataSourceModel{
		TeamID:  config.TeamID,
		Members: members,
	})...)
}
