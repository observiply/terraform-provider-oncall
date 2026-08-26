package provider

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ resource.Resource                = &triggerResource{}
	_ resource.ResourceWithConfigure   = &triggerResource{}
	_ resource.ResourceWithImportState = &triggerResource{}
)

func newTriggerResource() resource.Resource {
	return &triggerResource{}
}

type triggerResource struct {
	client *client.ClientWithResponses
}

type triggerResourceModel struct {
	ID                  types.String   `tfsdk:"id"`
	OwnerTeamID         types.String   `tfsdk:"owner_team_id"`
	TeamIDs             types.Set      `tfsdk:"team_ids"`
	VisibleToAllTeams   types.Bool     `tfsdk:"visible_to_all_teams"`
	Name                types.String   `tfsdk:"name"`
	Description         types.String   `tfsdk:"description"`
	Enabled             types.Bool     `tfsdk:"enabled"`
	AuthMethod          types.String   `tfsdk:"auth_method"`
	DedupKeyTemplate    types.String   `tfsdk:"dedup_key_template"`
	PayloadTemplate     types.String   `tfsdk:"payload_template"`
	ResolveWhenTemplate types.String   `tfsdk:"resolve_when_template"`
	StateTemplate       types.String   `tfsdk:"state_template"`
	IngestURL           types.String   `tfsdk:"ingest_url"`
	Token               types.String   `tfsdk:"token"`
	Timeouts            timeouts.Value `tfsdk:"timeouts"`
}

func (r *triggerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_trigger"
}

func (r *triggerResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an oncall trigger, an ingest endpoint that raises incidents. " +
			"See oncall_trigger_targets to configure what it notifies.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Trigger UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"owner_team_id": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Description: "Team that owns this trigger and alone controls sharing. The " +
					"oncall API has no move-between-owners operation, so changing this recreates the trigger.",
			},
			"team_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				Description: "Teams this trigger is shared with. Always includes owner_team_id.",
			},
			"visible_to_all_teams": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "If true, every team can view this trigger regardless of team_ids.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Trigger name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Trigger description.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the trigger currently accepts ingest events.",
			},
			"auth_method": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("none"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators:  []validator.String{stringvalidator.OneOf("none", "bearer")},
				Description: "Ingest auth method. Immutable after creation (the update API has no field for it).",
			},
			"dedup_key_template": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Template rendering the dedup key for an incoming event.",
			},
			"payload_template": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Template rendering incident fields from an incoming event.",
			},
			"resolve_when_template": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Template evaluating whether an incoming event resolves the incident.",
			},
			"state_template": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Template rendering the incident state from an incoming event.",
			},
			"ingest_url": schema.StringAttribute{
				Computed:      true,
				Description:   "URL to POST events to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"token": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "One-time bearer token for auth_method=bearer, returned only at " +
					"creation and never re-readable from the API. Empty for auth_method=none. " +
					"This resource does not rotate it — POST .../rotate-token also returns its " +
					"value exactly once, so unlike the integration secret there is no way to " +
					"manage rotation without landing a value in state; rotate out-of-band " +
					"instead (see tfprovider-08-secrets-and-rotation.md).",
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

func (r *triggerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *triggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *triggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan triggerResourceModel
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

	ownerID := plan.OwnerTeamID.ValueString()
	createBody, err := newBody((*client.PostAdminTeamsIdTriggersJSONBody).FromTriggersCreateTriggerBody, client.TriggersCreateTriggerBody{
		Name:                plan.Name.ValueStringPointer(),
		Description:         plan.Description.ValueStringPointer(),
		Enabled:             plan.Enabled.ValueBoolPointer(),
		AuthMethod:          plan.AuthMethod.ValueStringPointer(),
		DedupKeyTemplate:    plan.DedupKeyTemplate.ValueStringPointer(),
		PayloadTemplate:     plan.PayloadTemplate.ValueStringPointer(),
		ResolveWhenTemplate: plan.ResolveWhenTemplate.ValueStringPointer(),
		StateTemplate:       plan.StateTemplate.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode trigger create request", err.Error())
		return
	}
	createResp, err := r.client.PostAdminTeamsIdTriggersWithResponse(ctx, ownerID, client.PostAdminTeamsIdTriggersJSONRequestBody(createBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create trigger", err.Error())
		return
	}
	if createResp.StatusCode() != http.StatusCreated || createResp.JSON201 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to create trigger", "POST", "/admin/teams/"+ownerID+"/triggers", createResp.StatusCode(), createResp.Body))
		return
	}
	triggerID := *createResp.JSON201.Id

	state := triggerResourceModel{
		ID:                  types.StringValue(triggerID),
		OwnerTeamID:         strFromPtr(createResp.JSON201.OwnerTeamId),
		Name:                strFromPtr(createResp.JSON201.Name),
		Description:         strFromPtr(createResp.JSON201.Description),
		Enabled:             boolFromPtr(createResp.JSON201.Enabled),
		AuthMethod:          strFromPtr(createResp.JSON201.AuthMethod),
		DedupKeyTemplate:    strFromPtr(createResp.JSON201.DedupKeyTemplate),
		PayloadTemplate:     strFromPtr(createResp.JSON201.PayloadTemplate),
		ResolveWhenTemplate: strFromPtr(createResp.JSON201.ResolveWhenTemplate),
		StateTemplate:       strFromPtr(createResp.JSON201.StateTemplate),
		IngestURL:           strFromPtr(createResp.JSON201.IngestUrl),
		Token:               strFromPtr(createResp.JSON201.Token),
		Timeouts:            plan.Timeouts,
	}
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

	// The trigger now exists. Write state before the sharing call so a
	// failure below doesn't orphan it.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	visible := plan.VisibleToAllTeams.ValueBool()
	sharingBody, err := newBody((*client.PutAdminTriggersIdTeamsJSONBody).FromTriggersSharingBody, client.TriggersSharingBody{
		TeamIds:           &teamIDs,
		VisibleToAllTeams: &visible,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode trigger sharing request", err.Error())
		return
	}
	sharingResp, err := r.client.PutAdminTriggersIdTeamsWithResponse(ctx, triggerID, client.PutAdminTriggersIdTeamsJSONRequestBody(sharingBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to set trigger sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to set trigger sharing", "PUT", "/admin/triggers/"+triggerID+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}
	finalSet, d := stringsToSet(ctx, derefStrSlice(sharingResp.JSON200.TeamIds))
	resp.Diagnostics.Append(d...)
	state.TeamIDs = finalSet
	state.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *triggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state triggerResourceModel
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
	getResp, err := r.client.GetAdminTriggersIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read trigger", err.Error())
		return
	}
	if getResp.StatusCode() == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read trigger", "GET", "/admin/triggers/"+id, getResp.StatusCode(), getResp.Body))
		return
	}
	trig := getResp.JSON200
	state.OwnerTeamID = strFromPtr(trig.OwnerTeamId)
	state.Name = strFromPtr(trig.Name)
	state.Description = strFromPtr(trig.Description)
	state.Enabled = boolFromPtr(trig.Enabled)
	state.AuthMethod = strFromPtr(trig.AuthMethod)
	state.DedupKeyTemplate = strFromPtr(trig.DedupKeyTemplate)
	state.PayloadTemplate = strFromPtr(trig.PayloadTemplate)
	state.ResolveWhenTemplate = strFromPtr(trig.ResolveWhenTemplate)
	state.StateTemplate = strFromPtr(trig.StateTemplate)
	state.IngestURL = strFromPtr(trig.IngestUrl)
	// token is never returned by GET; state.Token keeps its prior value via
	// UseStateForUnknown.

	teamSet, d := stringsToSet(ctx, derefStrSlice(trig.TeamIds))
	resp.Diagnostics.Append(d...)
	state.TeamIDs = teamSet

	sharingResp, err := r.client.GetAdminTriggersIdTeamsWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read trigger sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read trigger sharing", "GET", "/admin/triggers/"+id+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}
	state.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *triggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state triggerResourceModel
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

	id := state.ID.ValueString()
	updateBody, err := newBody((*client.PutAdminTriggersIdJSONBody).FromTriggersUpdateTriggerBody, client.TriggersUpdateTriggerBody{
		Name:                plan.Name.ValueStringPointer(),
		Description:         plan.Description.ValueStringPointer(),
		Enabled:             plan.Enabled.ValueBoolPointer(),
		DedupKeyTemplate:    plan.DedupKeyTemplate.ValueStringPointer(),
		PayloadTemplate:     plan.PayloadTemplate.ValueStringPointer(),
		ResolveWhenTemplate: plan.ResolveWhenTemplate.ValueStringPointer(),
		StateTemplate:       plan.StateTemplate.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode trigger update request", err.Error())
		return
	}
	updateResp, err := r.client.PutAdminTriggersIdWithResponse(ctx, id, client.PutAdminTriggersIdJSONRequestBody(updateBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update trigger", err.Error())
		return
	}
	if updateResp.StatusCode() != http.StatusOK || updateResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to update trigger", "PUT", "/admin/triggers/"+id, updateResp.StatusCode(), updateResp.Body))
		return
	}

	newState := triggerResourceModel{
		ID:                  state.ID,
		OwnerTeamID:         strFromPtr(updateResp.JSON200.OwnerTeamId),
		Name:                strFromPtr(updateResp.JSON200.Name),
		Description:         strFromPtr(updateResp.JSON200.Description),
		Enabled:             boolFromPtr(updateResp.JSON200.Enabled),
		AuthMethod:          strFromPtr(updateResp.JSON200.AuthMethod),
		DedupKeyTemplate:    strFromPtr(updateResp.JSON200.DedupKeyTemplate),
		PayloadTemplate:     strFromPtr(updateResp.JSON200.PayloadTemplate),
		ResolveWhenTemplate: strFromPtr(updateResp.JSON200.ResolveWhenTemplate),
		StateTemplate:       strFromPtr(updateResp.JSON200.StateTemplate),
		IngestURL:           strFromPtr(updateResp.JSON200.IngestUrl),
		Token:               state.Token,
		TeamIDs:             state.TeamIDs,
		VisibleToAllTeams:   state.VisibleToAllTeams,
		Timeouts:            plan.Timeouts,
	}
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

	sharingBody, err := newBody((*client.PutAdminTriggersIdTeamsJSONBody).FromTriggersSharingBody, client.TriggersSharingBody{
		TeamIds:           &teamIDs,
		VisibleToAllTeams: &visible,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode trigger sharing request", err.Error())
		return
	}
	sharingResp, err := r.client.PutAdminTriggersIdTeamsWithResponse(ctx, id, client.PutAdminTriggersIdTeamsJSONRequestBody(sharingBody))
	if err != nil {
		resp.Diagnostics.AddError("Unable to set trigger sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to set trigger sharing", "PUT", "/admin/triggers/"+id+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}

	finalSet, d := stringsToSet(ctx, derefStrSlice(sharingResp.JSON200.TeamIds))
	resp.Diagnostics.Append(d...)
	newState.TeamIDs = finalSet
	newState.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *triggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state triggerResourceModel
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
	deleteResp, err := r.client.DeleteAdminTriggersIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete trigger", err.Error())
		return
	}
	if deleteResp.StatusCode() != http.StatusNoContent && deleteResp.StatusCode() != http.StatusNotFound {
		resp.Diagnostics.Append(unexpectedStatus("Unable to delete trigger", "DELETE", "/admin/triggers/"+id, deleteResp.StatusCode(), deleteResp.Body))
	}
}
