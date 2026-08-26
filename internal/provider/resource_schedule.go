package provider

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ resource.Resource                = &scheduleResource{}
	_ resource.ResourceWithConfigure   = &scheduleResource{}
	_ resource.ResourceWithImportState = &scheduleResource{}
)

func newScheduleResource() resource.Resource {
	return &scheduleResource{}
}

type scheduleResource struct {
	client *client.ClientWithResponses
}

type scheduleResourceModel struct {
	ID                types.String   `tfsdk:"id"`
	Name              types.String   `tfsdk:"name"`
	Description       types.String   `tfsdk:"description"`
	Timezone          types.String   `tfsdk:"timezone"`
	OwnerTeamID       types.String   `tfsdk:"owner_team_id"`
	TeamIDs           types.Set      `tfsdk:"team_ids"`
	VisibleToAllTeams types.Bool     `tfsdk:"visible_to_all_teams"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

func (r *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func (r *scheduleResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an oncall schedule. A schedule is a container for one or more " +
			"rotation layers (see oncall_schedule_layer) and a notification policy (see " +
			"oncall_schedule_notification_policy).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				Description:         "Schedule UUID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Schedule UUID.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Schedule name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Schedule description.",
			},
			"timezone": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("UTC"),
				Description: "IANA timezone (e.g. \"America/New_York\"). Defaults to UTC.",
			},
			"owner_team_id": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Description: "Team that owns this schedule and alone controls sharing " +
					"(AGENTS.md's sharing model). The oncall API has no move-between-owners " +
					"operation, so changing this recreates the schedule.",
			},
			"team_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				Description: "Teams this schedule is shared with. Always includes " +
					"owner_team_id; it is added automatically if omitted.",
			},
			"visible_to_all_teams": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "If true, every team can view this schedule regardless of team_ids.",
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

func (r *scheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleResourceModel
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
	configuredTeamIDs, d := setToStrings(ctx, plan.TeamIDs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	teamIDs := normalizeTeamIDs(ownerID, configuredTeamIDs)

	createBody, err := newBody((*client.PostAdminSchedulesJSONBody).FromAdminCreateScheduleBody, client.AdminCreateScheduleBody{
		Name:        plan.Name.ValueStringPointer(),
		Description: plan.Description.ValueStringPointer(),
		Timezone:    plan.Timezone.ValueStringPointer(),
		TeamIds:     &teamIDs,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode schedule create request", err.Error())
		return
	}
	createResp, err := r.client.PostAdminSchedulesWithBodyWithResponse(ctx, "application/json", createBody)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create schedule", err.Error())
		return
	}
	if createResp.StatusCode() != http.StatusCreated || createResp.JSON201 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to create schedule", "POST", "/admin/schedules", createResp.StatusCode(), createResp.Body))
		return
	}
	scheduleID := *createResp.JSON201.Id

	state := scheduleResourceModel{
		ID:          types.StringValue(scheduleID),
		Name:        strFromPtr(createResp.JSON201.Name),
		Description: strFromPtr(createResp.JSON201.Description),
		Timezone:    strFromPtr(createResp.JSON201.Timezone),
		OwnerTeamID: strFromPtr(createResp.JSON201.OwnerTeamId),
		Timeouts:    plan.Timeouts,
	}
	fallbackSet, d := stringsToSet(ctx, teamIDs)
	resp.Diagnostics.Append(d...)
	state.TeamIDs = fallbackSet
	state.VisibleToAllTeams = types.BoolValue(false)

	// The schedule now exists. Write state before the sharing call so a
	// failure below doesn't orphan it (tfprovider-07: "the single most
	// common way providers leak resources").
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// createScheduleBody has no visible_to_all_teams field, so the sharing
	// PUT is required even when team_ids is just [owner_team_id].
	visible := plan.VisibleToAllTeams.ValueBool()
	sharingBody, err := newBody((*client.PutAdminSchedulesIdTeamsJSONBody).FromAdminSharingBody, client.AdminSharingBody{
		TeamIds:           &teamIDs,
		VisibleToAllTeams: &visible,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode schedule sharing request", err.Error())
		return
	}
	sharingResp, err := r.client.PutAdminSchedulesIdTeamsWithBodyWithResponse(ctx, scheduleID, "application/json", sharingBody)
	if err != nil {
		resp.Diagnostics.AddError("Unable to set schedule sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to set schedule sharing", "PUT", "/admin/schedules/"+scheduleID+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}

	finalSet, d := stringsToSet(ctx, derefStrSlice(sharingResp.JSON200.TeamIds))
	resp.Diagnostics.Append(d...)
	state.TeamIDs = finalSet
	state.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleResourceModel
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
	getResp, err := r.client.GetAdminSchedulesIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule", err.Error())
		return
	}
	if getResp.StatusCode() == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read schedule", "GET", "/admin/schedules/"+id, getResp.StatusCode(), getResp.Body))
		return
	}
	sched := getResp.JSON200
	state.Name = strFromPtr(sched.Name)
	state.Description = strFromPtr(sched.Description)
	state.Timezone = strFromPtr(sched.Timezone)
	state.OwnerTeamID = strFromPtr(sched.OwnerTeamId)

	teamSet, d := stringsToSet(ctx, derefStrSlice(sched.TeamIds))
	resp.Diagnostics.Append(d...)
	state.TeamIDs = teamSet

	// visible_to_all_teams isn't part of scheduleResponse; only the sharing
	// endpoint carries it.
	sharingResp, err := r.client.GetAdminSchedulesIdTeamsWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read schedule sharing", "GET", "/admin/schedules/"+id+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}
	state.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state scheduleResourceModel
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
	updateBody, err := newBody((*client.PutAdminSchedulesIdJSONBody).FromAdminUpdateScheduleBody, client.AdminUpdateScheduleBody{
		Name:        plan.Name.ValueStringPointer(),
		Description: plan.Description.ValueStringPointer(),
		Timezone:    plan.Timezone.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode schedule update request", err.Error())
		return
	}
	updateResp, err := r.client.PutAdminSchedulesIdWithBodyWithResponse(ctx, id, "application/json", updateBody)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update schedule", err.Error())
		return
	}
	if updateResp.StatusCode() != http.StatusOK || updateResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to update schedule", "PUT", "/admin/schedules/"+id, updateResp.StatusCode(), updateResp.Body))
		return
	}

	newState := scheduleResourceModel{
		ID:                state.ID,
		Name:              strFromPtr(updateResp.JSON200.Name),
		Description:       strFromPtr(updateResp.JSON200.Description),
		Timezone:          strFromPtr(updateResp.JSON200.Timezone),
		OwnerTeamID:       strFromPtr(updateResp.JSON200.OwnerTeamId),
		TeamIDs:           state.TeamIDs,
		VisibleToAllTeams: state.VisibleToAllTeams,
		Timeouts:          plan.Timeouts,
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

	sharingBody, err := newBody((*client.PutAdminSchedulesIdTeamsJSONBody).FromAdminSharingBody, client.AdminSharingBody{
		TeamIds:           &teamIDs,
		VisibleToAllTeams: &visible,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode schedule sharing request", err.Error())
		return
	}
	sharingResp, err := r.client.PutAdminSchedulesIdTeamsWithBodyWithResponse(ctx, id, "application/json", sharingBody)
	if err != nil {
		resp.Diagnostics.AddError("Unable to set schedule sharing", err.Error())
		return
	}
	if sharingResp.StatusCode() != http.StatusOK || sharingResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to set schedule sharing", "PUT", "/admin/schedules/"+id+"/teams", sharingResp.StatusCode(), sharingResp.Body))
		return
	}

	finalSet, d := stringsToSet(ctx, derefStrSlice(sharingResp.JSON200.TeamIds))
	resp.Diagnostics.Append(d...)
	newState.TeamIDs = finalSet
	newState.VisibleToAllTeams = boolFromPtr(sharingResp.JSON200.VisibleToAllTeams)

	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleResourceModel
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
	deleteResp, err := r.client.DeleteAdminSchedulesIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete schedule", err.Error())
		return
	}
	if deleteResp.StatusCode() != http.StatusNoContent && deleteResp.StatusCode() != http.StatusNotFound {
		resp.Diagnostics.Append(unexpectedStatus("Unable to delete schedule", "DELETE", "/admin/schedules/"+id, deleteResp.StatusCode(), deleteResp.Body))
	}
}
