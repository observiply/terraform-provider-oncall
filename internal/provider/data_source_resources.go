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
	_ datasource.DataSource              = &resourcesDataSource{}
	_ datasource.DataSourceWithConfigure = &resourcesDataSource{}
)

func newResourcesDataSource() datasource.DataSource {
	return &resourcesDataSource{}
}

type resourcesDataSource struct {
	client *client.ClientWithResponses
}

type resourcesDataSourceModel struct {
	Resources []types.String `tfsdk:"resources"`
	Verbs     []types.String `tfsdk:"verbs"`
}

func (d *resourcesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resources"
}

func (d *resourcesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the RBAC resource names and verbs this oncall install knows about " +
			"(GET /admin/resources), useful for building a custom role's permissions list.",
		Attributes: map[string]schema.Attribute{
			"resources": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Every guarded resource name, e.g. \"schedules\", \"triggers\", \"integrations\".",
			},
			"verbs": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Every RBAC verb, e.g. \"view\", \"write\", \"delete\", \"share\".",
			},
		},
	}
}

func (d *resourcesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *resourcesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	getResp, err := d.client.GetAdminResourcesWithResponse(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read resources", err.Error())
		return
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read resources", "GET", "/admin/resources", getResp.StatusCode(), getResp.Body))
		return
	}

	toStrings := func(ss []string) []types.String {
		out := make([]types.String, len(ss))
		for i, s := range ss {
			out[i] = types.StringValue(s)
		}
		return out
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &resourcesDataSourceModel{
		Resources: toStrings(derefStrSlice(getResp.JSON200.Resources)),
		Verbs:     toStrings(derefStrSlice(getResp.JSON200.Verbs)),
	})...)
}
