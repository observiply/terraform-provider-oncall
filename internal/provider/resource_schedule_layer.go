package provider

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ resource.Resource                = &scheduleLayerResource{}
	_ resource.ResourceWithConfigure   = &scheduleLayerResource{}
	_ resource.ResourceWithImportState = &scheduleLayerResource{}
)

func newScheduleLayerResource() resource.Resource {
	return &scheduleLayerResource{}
}

type scheduleLayerResource struct {
	client *client.ClientWithResponses
}

type layerMemberModel struct {
	UserID   types.String `tfsdk:"user_id"`
	UserName types.String `tfsdk:"user_name"`
}

type layerRestrictionModel struct {
	// DayOfWeek is null for "every day". 0=Sun..6=Sat, matching the API.
	// Modeled one day per block (rather than the API's days []int on
	// restrictionInput) because restrictionResponse is already flattened to
	// one row per day server-side — grouping isn't preserved round-trip, so
	// a 1:1 block-to-row mapping is the only representation that Reads back
	// without inventing groupings the user didn't specify.
	DayOfWeek types.Int64  `tfsdk:"day_of_week"`
	StartTime types.String `tfsdk:"start_time"`
	EndTime   types.String `tfsdk:"end_time"`
}

type scheduleLayerResourceModel struct {
	ID             types.String            `tfsdk:"id"`
	ScheduleID     types.String            `tfsdk:"schedule_id"`
	Name           types.String            `tfsdk:"name"`
	Tier           types.Int64             `tfsdk:"tier"`
	RotationLength types.String            `tfsdk:"rotation_length"`
	HandoffAt      types.String            `tfsdk:"handoff_at"`
	StartAt        types.String            `tfsdk:"start_at"`
	EndAt          types.String            `tfsdk:"end_at"`
	Member         []layerMemberModel      `tfsdk:"member"`
	Restriction    []layerRestrictionModel `tfsdk:"restriction"`
	Timeouts       timeouts.Value          `tfsdk:"timeouts"`
}

func (r *scheduleLayerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule_layer"
}

func (r *scheduleLayerResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a rotation layer within an oncall_schedule.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Layer UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"schedule_id": schema.StringAttribute{
				Required:      true,
				Description:   "Schedule this layer belongs to. The API has no move-between-schedules operation.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Layer name.",
			},
			"tier": schema.Int64Attribute{
				Required:      true,
				Description:   "Escalation tier. Cannot be changed after creation.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
				Validators:    []validator.Int64{int64validator.AtLeast(1)},
			},
			"rotation_length": schema.StringAttribute{
				Required:    true,
				Description: "ISO 8601 duration (e.g. \"P1W\" for one week). All components must be >= 0 with at least one > 0.",
			},
			"handoff_at": schema.StringAttribute{
				Required:    true,
				Description: "RFC3339 timestamp of the first handoff.",
			},
			"start_at": schema.StringAttribute{
				Required:    true,
				Description: "RFC3339 timestamp the rotation begins.",
			},
			"end_at": schema.StringAttribute{
				Optional:    true,
				Description: "RFC3339 timestamp the rotation ends. Omit for no end.",
			},
		},
		Blocks: map[string]schema.Block{
			"member": schema.ListNestedBlock{
				Description: "One rotation member. Block order is the rotation order.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"user_id": schema.StringAttribute{
							Required:    true,
							Description: "Member's user UUID.",
						},
						"user_name": schema.StringAttribute{
							Computed:    true,
							Description: "Member's display name (not the email, which is PII-redacted for non-admins).",
						},
					},
				},
			},
			"restriction": schema.ListNestedBlock{
				Description: "One time-of-day/day-of-week restriction window.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"day_of_week": schema.Int64Attribute{
							Optional:    true,
							Description: "0=Sun..6=Sat. Omit for every day.",
							Validators:  []validator.Int64{int64validator.Between(0, 6)},
						},
						"start_time": schema.StringAttribute{
							Required:    true,
							Description: "\"HH:MM\" in the schedule's timezone.",
						},
						"end_time": schema.StringAttribute{
							Required:    true,
							Description: "\"HH:MM\" in the schedule's timezone.",
						},
					},
				},
			},
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				Read:   true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *scheduleLayerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *scheduleLayerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *scheduleLayerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleLayerResourceModel
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

	scheduleID := plan.ScheduleID.ValueString()
	createBody, err := newBody((*client.PostAdminSchedulesIdLayersJSONBody).FromAdminLayerBody, client.AdminLayerBody{
		Name:           plan.Name.ValueStringPointer(),
		Tier:           intPtrFromInt64(plan.Tier),
		RotationLength: plan.RotationLength.ValueStringPointer(),
		HandoffAt:      plan.HandoffAt.ValueStringPointer(),
		StartAt:        plan.StartAt.ValueStringPointer(),
		EndAt:          plan.EndAt.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode layer create request", err.Error())
		return
	}
	createResp, err := r.client.PostAdminSchedulesIdLayersWithBodyWithResponse(ctx, scheduleID, "application/json", createBody)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create schedule layer", err.Error())
		return
	}
	if createResp.StatusCode() != http.StatusCreated || createResp.JSON201 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to create schedule layer", "POST", "/admin/schedules/"+scheduleID+"/layers", createResp.StatusCode(), createResp.Body))
		return
	}
	layerID := *createResp.JSON201.Id

	state := layerRespToModel(createResp.JSON201, plan.Timeouts)
	state.Member = nil
	state.Restriction = nil

	// The layer now exists. Write state before the members/restrictions
	// calls so a failure below doesn't orphan it.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(plan.Member) > 0 {
		members, d := r.putMembers(ctx, layerID, plan.Member)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Member = members
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if len(plan.Restriction) > 0 {
		restrictions, d := r.putRestrictions(ctx, layerID, plan.Restriction)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Restriction = restrictions
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleLayerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleLayerResourceModel
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
	getResp, err := r.client.GetAdminLayersIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule layer", err.Error())
		return
	}
	if getResp.StatusCode() == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read schedule layer", "GET", "/admin/layers/"+id, getResp.StatusCode(), getResp.Body))
		return
	}
	newState := layerRespToModel(getResp.JSON200, state.Timeouts)

	members, d := r.readMembers(ctx, *getResp.JSON200.ScheduleId, id)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	newState.Member = members

	restrictionsResp, err := r.client.GetAdminLayersIdRestrictionsWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule layer restrictions", err.Error())
		return
	}
	if restrictionsResp.StatusCode() != http.StatusOK || restrictionsResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read schedule layer restrictions", "GET", "/admin/layers/"+id+"/restrictions", restrictionsResp.StatusCode(), restrictionsResp.Body))
		return
	}
	newState.Restriction = restrictionRespToModel(*restrictionsResp.JSON200)

	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *scheduleLayerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state scheduleLayerResourceModel
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
	updateBody, err := newBody((*client.PutAdminLayersIdJSONBody).FromAdminUpdateLayerBody, client.AdminUpdateLayerBody{
		Name:           plan.Name.ValueStringPointer(),
		RotationLength: plan.RotationLength.ValueStringPointer(),
		HandoffAt:      plan.HandoffAt.ValueStringPointer(),
		StartAt:        plan.StartAt.ValueStringPointer(),
		EndAt:          plan.EndAt.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode layer update request", err.Error())
		return
	}
	updateResp, err := r.client.PutAdminLayersIdWithBodyWithResponse(ctx, id, "application/json", updateBody)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update schedule layer", err.Error())
		return
	}
	if updateResp.StatusCode() != http.StatusOK || updateResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to update schedule layer", "PUT", "/admin/layers/"+id, updateResp.StatusCode(), updateResp.Body))
		return
	}
	newState := layerRespToModel(updateResp.JSON200, plan.Timeouts)
	newState.Member = state.Member
	newState.Restriction = state.Restriction
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
	if resp.Diagnostics.HasError() {
		return
	}

	members, d := r.putMembers(ctx, id, plan.Member)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	newState.Member = members
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
	if resp.Diagnostics.HasError() {
		return
	}

	restrictions, d := r.putRestrictions(ctx, id, plan.Restriction)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	newState.Restriction = restrictions

	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *scheduleLayerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleLayerResourceModel
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
	deleteResp, err := r.client.DeleteAdminLayersIdWithResponse(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete schedule layer", err.Error())
		return
	}
	if deleteResp.StatusCode() != http.StatusNoContent && deleteResp.StatusCode() != http.StatusNotFound {
		resp.Diagnostics.Append(unexpectedStatus("Unable to delete schedule layer", "DELETE", "/admin/layers/"+id, deleteResp.StatusCode(), deleteResp.Body))
	}
}

// putMembers atomically replaces a layer's roster, using plan order to
// derive each member's position (memberInputBody.position is a distinct
// server field, not implied by array index).
// readMembers finds layerID's roster from the schedule-level list-layers
// response — there's no single-layer members GET, only the per-schedule one
// used to list layers with their members embedded.
func (r *scheduleLayerResource) readMembers(ctx context.Context, scheduleID, layerID string) ([]layerMemberModel, diag.Diagnostics) {
	membersResp, err := r.client.GetAdminSchedulesIdLayersWithResponse(ctx, scheduleID)
	if err != nil {
		return nil, diagsFromError("Unable to read schedule layer members", err)
	}
	if membersResp.StatusCode() != http.StatusOK || membersResp.JSON200 == nil {
		return nil, diagsFromDiagnostic(unexpectedStatus("Unable to read schedule layer members", "GET", "/admin/schedules/"+scheduleID+"/layers", membersResp.StatusCode(), membersResp.Body))
	}
	for _, l := range *membersResp.JSON200 {
		if l.Id != nil && *l.Id == layerID {
			return memberRespToModel(l.Members), nil
		}
	}
	return nil, nil
}

func (r *scheduleLayerResource) putMembers(ctx context.Context, layerID string, plan []layerMemberModel) ([]layerMemberModel, diag.Diagnostics) {
	inputs := make([]client.AdminMemberInputBody, len(plan))
	for i, m := range plan {
		pos := i
		inputs[i] = client.AdminMemberInputBody{
			UserId:   m.UserID.ValueStringPointer(),
			Position: &pos,
		}
	}
	body, err := newBody((*client.PutAdminLayersIdMembersJSONBody).FromPutAdminLayersIdMembersJSONBody1, inputs)
	if err != nil {
		return nil, diagsFromError("Unable to encode layer members request", err)
	}
	membersResp, err := r.client.PutAdminLayersIdMembersWithBodyWithResponse(ctx, layerID, "application/json", body)
	if err != nil {
		return nil, diagsFromError("Unable to set layer members", err)
	}
	if membersResp.StatusCode() != http.StatusOK || membersResp.JSON200 == nil {
		return nil, diagsFromDiagnostic(unexpectedStatus("Unable to set layer members", "PUT", "/admin/layers/"+layerID+"/members", membersResp.StatusCode(), membersResp.Body))
	}
	return memberRespToModel(membersResp.JSON200), nil
}

// putRestrictions atomically replaces a layer's time restrictions. Each
// plan block is a single day (or "every day" when day_of_week is unset), so
// it maps 1:1 onto restrictionInput/restrictionResponse without grouping.
func (r *scheduleLayerResource) putRestrictions(ctx context.Context, layerID string, plan []layerRestrictionModel) ([]layerRestrictionModel, diag.Diagnostics) {
	inputs := make([]client.AdminRestrictionInput, len(plan))
	for i, rst := range plan {
		var days *[]int
		if !rst.DayOfWeek.IsNull() {
			d := []int{int(rst.DayOfWeek.ValueInt64())}
			days = &d
		}
		inputs[i] = client.AdminRestrictionInput{
			Days:      days,
			StartTime: rst.StartTime.ValueStringPointer(),
			EndTime:   rst.EndTime.ValueStringPointer(),
		}
	}
	body, err := newBody((*client.PutAdminLayersIdRestrictionsJSONBody).FromPutAdminLayersIdRestrictionsJSONBody1, inputs)
	if err != nil {
		return nil, diagsFromError("Unable to encode layer restrictions request", err)
	}
	restrictionsResp, err := r.client.PutAdminLayersIdRestrictionsWithBodyWithResponse(ctx, layerID, "application/json", body)
	if err != nil {
		return nil, diagsFromError("Unable to set layer restrictions", err)
	}
	if restrictionsResp.StatusCode() != http.StatusOK || restrictionsResp.JSON200 == nil {
		return nil, diagsFromDiagnostic(unexpectedStatus("Unable to set layer restrictions", "PUT", "/admin/layers/"+layerID+"/restrictions", restrictionsResp.StatusCode(), restrictionsResp.Body))
	}
	return restrictionRespToModel(*restrictionsResp.JSON200), nil
}

func layerRespToModel(l *client.AdminLayerResp, tf timeouts.Value) scheduleLayerResourceModel {
	return scheduleLayerResourceModel{
		ID:             strFromPtr(l.Id),
		ScheduleID:     strFromPtr(l.ScheduleId),
		Name:           strFromPtr(l.Name),
		Tier:           int64FromIntPtr(l.Tier),
		RotationLength: intervalToISO8601(l.RotationLength),
		HandoffAt:      strFromPtr(l.HandoffAt),
		StartAt:        strFromPtr(l.StartAt),
		EndAt:          strFromPtr(l.EndAt),
		Timeouts:       tf,
	}
}

// intervalToISO8601 converts the API's calendar-aware interval object into a
// stable Terraform string. Whole-week day counts use W so the documented P1W
// and P2W configurations round-trip without a perpetual diff.
func intervalToISO8601(iv *client.PgintervalInterval) types.String {
	if iv == nil {
		return types.StringNull()
	}
	months := int64FromInt(iv.Months)
	days := int64FromInt(iv.Days)
	micros := int64FromInt(iv.Micros)

	if months == 0 && days > 0 && days%7 == 0 && micros == 0 {
		return types.StringValue("P" + strconv.FormatInt(days/7, 10) + "W")
	}

	var out strings.Builder
	out.WriteByte('P')
	if months != 0 {
		out.WriteString(strconv.FormatInt(months, 10))
		out.WriteByte('M')
	}
	if days != 0 {
		out.WriteString(strconv.FormatInt(days, 10))
		out.WriteByte('D')
	}
	if micros != 0 {
		out.WriteByte('T')
		hours := micros / 3_600_000_000
		micros %= 3_600_000_000
		minutes := micros / 60_000_000
		micros %= 60_000_000
		if hours != 0 {
			out.WriteString(strconv.FormatInt(hours, 10))
			out.WriteByte('H')
		}
		if minutes != 0 {
			out.WriteString(strconv.FormatInt(minutes, 10))
			out.WriteByte('M')
		}
		if micros != 0 {
			seconds := strconv.FormatInt(micros/1_000_000, 10)
			fraction := strings.TrimRight(fmt.Sprintf("%06d", micros%1_000_000), "0")
			out.WriteString(seconds)
			if fraction != "" {
				out.WriteByte('.')
				out.WriteString(fraction)
			}
			out.WriteByte('S')
		}
	}
	if out.Len() == 1 {
		return types.StringValue("PT0S")
	}
	return types.StringValue(out.String())
}

func int64FromInt(value *int) int64 {
	if value == nil {
		return 0
	}
	return int64(*value)
}

func memberRespToModel(members *[]client.AdminMemberResp) []layerMemberModel {
	if members == nil {
		return nil
	}
	out := make([]layerMemberModel, len(*members))
	for i, m := range *members {
		out[i] = layerMemberModel{
			UserID:   strFromPtr(m.UserId),
			UserName: strFromPtr(m.UserName),
		}
	}
	return out
}

func restrictionRespToModel(restrictions []client.AdminRestrictionResponse) []layerRestrictionModel {
	out := make([]layerRestrictionModel, len(restrictions))
	for i, rst := range restrictions {
		out[i] = layerRestrictionModel{
			DayOfWeek: int64FromIntPtr(rst.DayOfWeek),
			StartTime: strFromPtr(rst.StartTime),
			EndTime:   strFromPtr(rst.EndTime),
		}
	}
	return out
}
