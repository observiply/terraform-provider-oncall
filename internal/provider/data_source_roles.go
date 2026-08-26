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
	_ datasource.DataSource              = &rolesDataSource{}
	_ datasource.DataSourceWithConfigure = &rolesDataSource{}
)

func newRolesDataSource() datasource.DataSource {
	return &rolesDataSource{}
}

type rolesDataSource struct {
	client *client.ClientWithResponses
}

type rolePermEntryModel struct {
	Resource types.String `tfsdk:"resource"`
	Verb     types.String `tfsdk:"verb"`
}

type roleSummaryModel struct {
	ID          types.String         `tfsdk:"id"`
	Name        types.String         `tfsdk:"name"`
	Description types.String         `tfsdk:"description"`
	Builtin     types.Bool           `tfsdk:"builtin"`
	TeamID      types.String         `tfsdk:"team_id"`
	Permissions []rolePermEntryModel `tfsdk:"permissions"`
}

type rolesDataSourceModel struct {
	Roles []roleSummaryModel `tfsdk:"roles"`
}

func (d *rolesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_roles"
}

func (d *rolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists oncall's built-in roles (viewer, editor, owner, global_viewer). " +
			"Per-team custom roles are out of scope for this data source (RBAC/custom-role " +
			"management is admin-only, per AGENTS.md's RBAC section).",
		Attributes: map[string]schema.Attribute{
			"roles": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true, Description: "Role UUID."},
						"name":        schema.StringAttribute{Computed: true, Description: "Role name."},
						"description": schema.StringAttribute{Computed: true, Description: "Role description."},
						"builtin":     schema.BoolAttribute{Computed: true, Description: "True for the built-in viewer/editor/owner/global_viewer roles."},
						"team_id":     schema.StringAttribute{Computed: true, Description: "Empty for built-in roles, which use the `*` wildcard resource."},
						"permissions": schema.ListNestedAttribute{
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"resource": schema.StringAttribute{Computed: true, Description: "Resource name, or \"*\" for every resource."},
									"verb":     schema.StringAttribute{Computed: true, Description: "One of view/write/delete/share."},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *rolesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *rolesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	listResp, err := d.client.GetAdminRolesWithResponse(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list roles", err.Error())
		return
	}
	if listResp.StatusCode() != http.StatusOK || listResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to list roles", "GET", "/admin/roles", listResp.StatusCode(), listResp.Body))
		return
	}

	roles := make([]roleSummaryModel, len(*listResp.JSON200))
	for i, r := range *listResp.JSON200 {
		var perms []rolePermEntryModel
		if r.Permissions != nil {
			perms = make([]rolePermEntryModel, len(*r.Permissions))
			for j, p := range *r.Permissions {
				perms[j] = rolePermEntryModel{
					Resource: strFromPtr(p.Resource),
					Verb:     strFromPtr(p.Verb),
				}
			}
		}
		roles[i] = roleSummaryModel{
			ID:          strFromPtr(r.Id),
			Name:        strFromPtr(r.Name),
			Description: strFromPtr(r.Description),
			Builtin:     boolFromPtr(r.Builtin),
			TeamID:      strFromPtr(r.TeamId),
			Permissions: perms,
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &rolesDataSourceModel{Roles: roles})...)
}
