package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ resource.Resource                = &integrationResource{}
	_ resource.ResourceWithConfigure   = &integrationResource{}
	_ resource.ResourceWithImportState = &integrationResource{}
)

func newIntegrationResource() resource.Resource {
	return &integrationResource{}
}

type integrationResource struct {
	client *client.ClientWithResponses
}

type integrationResourceModel struct {
	ID                      types.String         `tfsdk:"id"`
	OwnerTeamID             types.String         `tfsdk:"owner_team_id"`
	TeamIDs                 types.Set            `tfsdk:"team_ids"`
	VisibleToAllTeams       types.Bool           `tfsdk:"visible_to_all_teams"`
	Name                    types.String         `tfsdk:"name"`
	Description             types.String         `tfsdk:"description"`
	Kind                    types.String         `tfsdk:"kind"`
	Enabled                 types.Bool           `tfsdk:"enabled"`
	URL                     types.String         `tfsdk:"url"`
	HTTPMethod              types.String         `tfsdk:"http_method"`
	Headers                 jsontypes.Normalized `tfsdk:"headers"`
	PayloadTemplate         types.String         `tfsdk:"payload_template"`
	ReminderHeaders         jsontypes.Normalized `tfsdk:"reminder_headers"`
	ReminderPayloadTemplate types.String         `tfsdk:"reminder_payload_template"`
	AuthMethod              types.String         `tfsdk:"auth_method"`
	HasSecret               types.Bool           `tfsdk:"has_secret"`
	OwnerUserID             types.String         `tfsdk:"owner_user_id"`
	SecretWO                types.String         `tfsdk:"secret_wo"`
	SecretWOVersion         types.Int64          `tfsdk:"secret_wo_version"`
	Timeouts                timeouts.Value       `tfsdk:"timeouts"`
}

func (r *integrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration"
}

func (r *integrationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an oncall integration, an outbound notification target " +
			"(e.g. a webhook) referenced by trigger targets and notification policy steps. " +
			"The integration's secret is set via secret_wo/secret_wo_version, a write-only " +
			"attribute pair (Terraform >= 1.11) — the API never returns it, so has_secret " +
			"only reports whether one is currently set.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Integration UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"owner_team_id": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Description: "Team that owns this integration and alone controls sharing. The " +
					"oncall API has no move-between-owners operation, so changing this recreates the integration.",
			},
			"team_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				Description: "Teams this integration is shared with. Always includes owner_team_id.",
			},
			"visible_to_all_teams": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "If true, every team can view this integration regardless of team_ids.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Integration name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Integration description.",
			},
			"kind": schema.StringAttribute{
				Required:    true,
				Description: "Integration kind, e.g. \"outgoing_webhook\".",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the integration is currently active.",
			},
			"url": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Target URL for a webhook-style integration.",
			},
			"http_method": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("POST"),
				Validators:  []validator.String{stringvalidator.OneOf("GET", "POST", "PUT", "PATCH", "DELETE")},
				Description: "HTTP method used to call url.",
			},
			"headers": schema.StringAttribute{
				CustomType:  jsontypes.NormalizedType{},
				Optional:    true,
				Computed:    true,
				Description: "JSON object of extra HTTP headers to send.",
			},
			"payload_template": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Template rendering the outbound request body.",
			},
			"reminder_headers": schema.StringAttribute{
				CustomType:  jsontypes.NormalizedType{},
				Optional:    true,
				Computed:    true,
				Description: "JSON object of extra HTTP headers to send for shift-reminder deliveries.",
			},
			"reminder_payload_template": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Template rendering the outbound request body for shift-reminder deliveries.",
			},
			"auth_method": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("none"),
				Validators:  []validator.String{stringvalidator.OneOf("none", "bearer", "basic")},
				Description: "Outbound auth method. The credential itself (tfprovider-08) is set separately.",
			},
			"has_secret": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether an outbound auth secret is currently set.",
			},
			"owner_user_id": schema.StringAttribute{
				Computed:    true,
				Description: "Set instead of a team owner for a personal integration; always null for integrations created through this resource.",
			},
			"secret_wo": schema.StringAttribute{
				Optional:    true,
				WriteOnly:   true,
				Description: "Outbound auth secret for auth_method=bearer/basic. Never read back from the API and never persisted to state; bump secret_wo_version to send a new value.",
			},
			"secret_wo_version": schema.Int64Attribute{
				Optional: true,
				Description: "Bump this to re-send secret_wo. The API has no way to read a secret back, so this " +
					"is the only signal the provider has that the value changed; changing secret_wo alone does nothing.",
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				Read:   true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *integrationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*OncallProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *provider.OncallProviderData, got: %T", req.ProviderData),
		)
		return
	}
	r.client = data.Client
}

func (r *integrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *integrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan integrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, defaultOperationTimeoutSeconds*time.Second)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	headers, err := normalizedToJSONMap(plan.Headers)
	if err != nil {
		resp.Diagnostics.AddError("Invalid headers", err.Error())
		return
	}
	reminderHeaders, err := normalizedToJSONMap(plan.ReminderHeaders)
	if err != nil {
		resp.Diagnostics.AddError("Invalid reminder_headers", err.Error())
		return
	}

	ownerID := plan.OwnerTeamID.ValueString()
	createBody, err := newBody((*client.PostAdminTeamsIdIntegrationsJSONBody).FromIntegrationsCreateBody, client.IntegrationsCreateBody{
		Name:                    plan.Name.ValueStringPointer(),
		Description:             plan.Description.ValueStringPointer(),
		Kind:                    plan.Kind.ValueStringPointer(),
		Enabled:                 plan.Enabled.ValueBoolPointer(),
		Url:                     plan.URL.ValueStringPointer(),
		HttpMethod:              plan.HTTPMethod.ValueStringPointer(),
		Headers:                 headers,
		PayloadTemplate:         plan.PayloadTemplate.ValueStringPointer(),
		ReminderHeaders:         reminderHeaders,
		ReminderPayloadTemplate: plan.ReminderPayloadTemplate.ValueStringPointer(),
		AuthMethod:              plan.AuthMethod.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode integration create request", err.Error())
		return
	}
	createResp, err := r.client.PostAdminTeamsIdIntegrationsWithResponse(ctx, ownerID, client.PostAdminTeamsIdIntegrationsJSONRequestBody(createBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create integration", err.Error())
		return
	}
	if createResp.StatusCode() != http.StatusCreated || createResp.JSON201 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to create integration", "POST", "/admin/teams/"+ownerID+"/integrations", createResp.StatusCode(), createResp.Body))
		return
	}
	integrationID := *createResp.JSON201.Id

	state := integrationRespToModel(createResp.JSON201, plan.Timeouts)

	configuredTeamIDs, d := setToStrings(ctx, plan.TeamIDs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	teamIDs := normalizeTeamIDs(ownerID, configuredTeamIDs)
	fallbackSet, d := stringsToSet(ctx, teamIDs)
	resp.Diagnostics.Append(d...)
	state.TeamIDs = fallbackSet
	state.VisibleToAllTeams = types.BoolValue(false)

	// The integration now exists. Write state before the sharing call so a
	// failure below doesn't orphan it.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	visible := plan.VisibleToAllTeams.ValueBool()
	sharingBody, err := newBody((*client.PutAdminIntegrationsIdTeamsJSONBody).FromIntegrationsSharingBody, client.IntegrationsSharingBody{
		TeamIds:           &teamIDs,
		VisibleToAllTeams: &visible,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode integration sharing request", err.Error())
		return
	}
	sharingResp, err := r.client.PutAdminIntegrationsIdTeamsWithResponse(ctx, integrationID, client.PutAdminIntegrationsIdTeamsJSONRequestBody(sharingBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to set integration sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to set integration sharing", "PUT", "/admin/integrations/"+integrationID+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}
	finalSet, d := stringsToSet(ctx, derefStrSlice(sharingResp.JSON200.TeamIds))
	resp.Diagnostics.Append(d...)
	state.TeamIDs = finalSet
	state.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(r.syncSecret(ctx, req.Config, integrationID, plan.SecretWOVersion, types.Int64Null(), &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *integrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state integrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, diags := state.Timeouts.Read(ctx, defaultOperationTimeoutSeconds*time.Second)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	id := state.ID.ValueString()
	getResp, err := r.client.GetAdminIntegrationsIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read integration", err.Error())
		return
	}
	if getResp.StatusCode() == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read integration", "GET", "/admin/integrations/"+id, getResp.StatusCode(), getResp.Body))
		return
	}

	newState := integrationRespToModel(getResp.JSON200, state.Timeouts)
	teamSet, d := stringsToSet(ctx, derefStrSlice(getResp.JSON200.TeamIds))
	resp.Diagnostics.Append(d...)
	newState.TeamIDs = teamSet
	// secret_wo is never returned by GET; secret_wo_version has no server-side
	// counterpart to reconcile against, so it just carries forward from state.
	newState.SecretWOVersion = state.SecretWOVersion

	sharingResp, err := r.client.GetAdminIntegrationsIdTeamsWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read integration sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read integration sharing", "GET", "/admin/integrations/"+id+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}
	newState.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *integrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state integrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, diags := plan.Timeouts.Update(ctx, defaultOperationTimeoutSeconds*time.Second)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	headers, err := normalizedToJSONMap(plan.Headers)
	if err != nil {
		resp.Diagnostics.AddError("Invalid headers", err.Error())
		return
	}
	reminderHeaders, err := normalizedToJSONMap(plan.ReminderHeaders)
	if err != nil {
		resp.Diagnostics.AddError("Invalid reminder_headers", err.Error())
		return
	}

	id := state.ID.ValueString()
	updateBody, err := newBody((*client.PutAdminIntegrationsIdJSONBody).FromIntegrationsUpdateBody, client.IntegrationsUpdateBody{
		Name:                    plan.Name.ValueStringPointer(),
		Description:             plan.Description.ValueStringPointer(),
		Kind:                    plan.Kind.ValueStringPointer(),
		Enabled:                 plan.Enabled.ValueBoolPointer(),
		Url:                     plan.URL.ValueStringPointer(),
		HttpMethod:              plan.HTTPMethod.ValueStringPointer(),
		Headers:                 headers,
		PayloadTemplate:         plan.PayloadTemplate.ValueStringPointer(),
		ReminderHeaders:         reminderHeaders,
		ReminderPayloadTemplate: plan.ReminderPayloadTemplate.ValueStringPointer(),
		AuthMethod:              plan.AuthMethod.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode integration update request", err.Error())
		return
	}
	updateResp, err := r.client.PutAdminIntegrationsIdWithResponse(ctx, id, client.PutAdminIntegrationsIdJSONRequestBody(updateBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update integration", err.Error())
		return
	}
	if updateResp.StatusCode() != http.StatusOK || updateResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to update integration", "PUT", "/admin/integrations/"+id, updateResp.StatusCode(), updateResp.Body))
		return
	}

	newState := integrationRespToModel(updateResp.JSON200, plan.Timeouts)
	newState.TeamIDs = state.TeamIDs
	newState.VisibleToAllTeams = state.VisibleToAllTeams
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ownerID := plan.OwnerTeamID.ValueString()
	desiredTeamIDs, d := setToStrings(ctx, plan.TeamIDs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	teamIDs := normalizeTeamIDs(ownerID, desiredTeamIDs)
	visible := plan.VisibleToAllTeams.ValueBool()

	sharingBody, err := newBody((*client.PutAdminIntegrationsIdTeamsJSONBody).FromIntegrationsSharingBody, client.IntegrationsSharingBody{
		TeamIds:           &teamIDs,
		VisibleToAllTeams: &visible,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode integration sharing request", err.Error())
		return
	}
	sharingResp, err := r.client.PutAdminIntegrationsIdTeamsWithResponse(ctx, id, client.PutAdminIntegrationsIdTeamsJSONRequestBody(sharingBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to set integration sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to set integration sharing", "PUT", "/admin/integrations/"+id+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}

	finalSet, d := stringsToSet(ctx, derefStrSlice(sharingResp.JSON200.TeamIds))
	resp.Diagnostics.Append(d...)
	newState.TeamIDs = finalSet
	newState.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(r.syncSecret(ctx, req.Config, id, plan.SecretWOVersion, state.SecretWOVersion, &newState)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

// syncSecret sends secret_wo to PUT /admin/integrations/{id}/secret when
// wantVersion differs from haveVersion — the only signal the provider has
// that the write-only value changed, since the API never returns it to diff
// against. On success it updates state's has_secret/secret_wo_version; on
// failure it returns a diagnostic and leaves state untouched so the caller
// still persists whatever else succeeded this apply.
func (r *integrationResource) syncSecret(ctx context.Context, cfg tfsdk.Config, id string, wantVersion, haveVersion types.Int64, state *integrationResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if wantVersion.IsNull() || wantVersion.Equal(haveVersion) {
		return diags
	}

	var cfgModel integrationResourceModel
	diags.Append(cfg.Get(ctx, &cfgModel)...)
	if diags.HasError() {
		return diags
	}

	body, err := newBody((*client.PutAdminIntegrationsIdSecretJSONBody).FromIntegrationsSetSecretBody, client.IntegrationsSetSecretBody{
		Secret: cfgModel.SecretWO.ValueStringPointer(),
	})
	if err != nil {
		diags.AddError("Unable to encode integration secret request", err.Error())
		return diags
	}
	secretResp, err := r.client.PutAdminIntegrationsIdSecretWithResponse(ctx, id, client.PutAdminIntegrationsIdSecretJSONRequestBody(body))
	if err != nil {
		diags.AddError("Unable to set integration secret", err.Error())
		return diags
	}
	if secretResp.StatusCode() != http.StatusNoContent {
		diags.Append(unexpectedStatus("Unable to set integration secret", "PUT", "/admin/integrations/"+id+"/secret", secretResp.StatusCode(), secretResp.Body))
		return diags
	}

	state.HasSecret = types.BoolValue(true)
	state.SecretWOVersion = wantVersion
	return diags
}

func (r *integrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state integrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, diags := state.Timeouts.Delete(ctx, defaultOperationTimeoutSeconds*time.Second)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	id := state.ID.ValueString()
	deleteResp, err := r.client.DeleteAdminIntegrationsIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete integration", err.Error())
		return
	}
	if deleteResp.StatusCode() != http.StatusNoContent && deleteResp.StatusCode() != http.StatusNotFound {
		resp.Diagnostics.Append(unexpectedStatus("Unable to delete integration", "DELETE", "/admin/integrations/"+id, deleteResp.StatusCode(), deleteResp.Body))
	}
}

func integrationRespToModel(i *client.IntegrationsIntegrationResponse, tf timeouts.Value) integrationResourceModel {
	return integrationResourceModel{
		ID:                      strFromPtr(i.Id),
		OwnerTeamID:             strFromPtr(i.OwnerTeamId),
		Name:                    strFromPtr(i.Name),
		Description:             strFromPtr(i.Description),
		Kind:                    strFromPtr(i.Kind),
		Enabled:                 boolFromPtr(i.Enabled),
		URL:                     strFromPtr(i.Url),
		HTTPMethod:              strFromPtr(i.HttpMethod),
		Headers:                 jsonMapToNormalized(i.Headers),
		PayloadTemplate:         strFromPtr(i.PayloadTemplate),
		ReminderHeaders:         jsonMapToNormalized(i.ReminderHeaders),
		ReminderPayloadTemplate: strFromPtr(i.ReminderPayloadTemplate),
		AuthMethod:              strFromPtr(i.AuthMethod),
		HasSecret:               boolFromPtr(i.HasSecret),
		OwnerUserID:             strFromPtr(i.OwnerUserId),
		Timeouts:                tf,
	}
}

// jsonMapToNormalized converts a decoded API response map into the
// jsontypes.Normalized JSON-string representation used by the schema, so
// key-order/whitespace differences from the API never show up as a diff.
func jsonMapToNormalized(m *map[string]interface{}) jsontypes.Normalized {
	if m == nil {
		return jsontypes.NewNormalizedNull()
	}
	b, err := json.Marshal(*m)
	if err != nil {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(b))
}

// normalizedToJSONMap decodes a jsontypes.Normalized attribute value into
// the map the generated client expects, returning nil (field omitted) for
// null/unknown/empty.
func normalizedToJSONMap(v jsontypes.Normalized) (*map[string]interface{}, error) {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(v.ValueString()), &m); err != nil {
		return nil, err
	}
	return &m, nil
}
