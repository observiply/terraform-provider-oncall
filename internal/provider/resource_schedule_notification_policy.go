package provider

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ resource.Resource                = &scheduleNotificationPolicyResource{}
	_ resource.ResourceWithConfigure   = &scheduleNotificationPolicyResource{}
	_ resource.ResourceWithImportState = &scheduleNotificationPolicyResource{}
)

func newScheduleNotificationPolicyResource() resource.Resource {
	return &scheduleNotificationPolicyResource{}
}

type scheduleNotificationPolicyResource struct {
	client *client.ClientWithResponses
}

type notificationStepModel struct {
	StepType         types.String `tfsdk:"step_type"`
	Tier             types.Int64  `tfsdk:"tier"`
	UserID           types.String `tfsdk:"user_id"`
	IntegrationID    types.String `tfsdk:"integration_id"`
	RepeatCount      types.Int64  `tfsdk:"repeat_count"`
	WaitAfterSeconds types.Int64  `tfsdk:"wait_after_seconds"`
}

type scheduleNotificationPolicyResourceModel struct {
	ID         types.String            `tfsdk:"id"`
	ScheduleID types.String            `tfsdk:"schedule_id"`
	Step       []notificationStepModel `tfsdk:"step"`
	Timeouts   timeouts.Value          `tfsdk:"timeouts"`
}

func (r *scheduleNotificationPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule_notification_policy"
}

func (r *scheduleNotificationPolicyResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the notification policy for an oncall_schedule. This is a " +
			"singleton per schedule: the ordered list of steps is atomically replaced on " +
			"every apply (PUT /admin/schedules/{id}/notification-policy has no separate " +
			"create). Destroying this resource clears the policy (PUT with an empty list).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Same value as schedule_id; this resource is keyed 1:1 to a schedule.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"schedule_id": schema.StringAttribute{
				Required:      true,
				Description:   "Schedule this notification policy belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
		Blocks: map[string]schema.Block{
			"step": schema.ListNestedBlock{
				Description: "One notification step. Block order is the escalation order.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"step_type": schema.StringAttribute{
							Required: true,
							Description: "One of \"layer\" (notify the oncall for a tier; requires tier), " +
								"\"static_user\" (requires user_id), \"integration\" (requires integration_id), " +
								"or \"repeat\" (requires repeat_count).",
							Validators: []validator.String{
								stringvalidator.OneOf("layer", "static_user", "integration", "repeat"),
							},
						},
						"tier": schema.Int64Attribute{
							Optional:    true,
							Description: "Escalation tier. Required (>= 1) when step_type = \"layer\".",
						},
						"user_id": schema.StringAttribute{
							Optional:    true,
							Description: "User UUID. Required when step_type = \"static_user\".",
						},
						"integration_id": schema.StringAttribute{
							Optional:    true,
							Description: "Integration UUID. Required when step_type = \"integration\".",
						},
						"repeat_count": schema.Int64Attribute{
							Optional:    true,
							Description: "Number of times to repeat. Required (>= 1) when step_type = \"repeat\".",
						},
						"wait_after_seconds": schema.Int64Attribute{
							Optional:    true,
							Computed:    true,
							Default:     int64default.StaticInt64(0),
							Description: "Seconds to wait after this step before continuing (0-3600).",
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

func (r *scheduleNotificationPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *scheduleNotificationPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The resource is keyed by schedule_id; both id and schedule_id are the
	// imported schedule UUID.
	resource.ImportStatePassthroughID(ctx, path.Root("schedule_id"), req, resp)
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *scheduleNotificationPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleNotificationPolicyResourceModel
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
	steps, d := r.putSteps(ctx, scheduleID, plan.Step)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := scheduleNotificationPolicyResourceModel{
		ID:         types.StringValue(scheduleID),
		ScheduleID: types.StringValue(scheduleID),
		Step:       steps,
		Timeouts:   plan.Timeouts,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleNotificationPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleNotificationPolicyResourceModel
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

	scheduleID := state.ScheduleID.ValueString()
	getResp, err := r.client.GetAdminSchedulesIdNotificationPolicyWithResponse(ctx, scheduleID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule notification policy", err.Error())
		return
	}
	if getResp.StatusCode() == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		resp.Diagnostics.Append(unexpectedStatus("Unable to read schedule notification policy", "GET", "/admin/schedules/"+scheduleID+"/notification-policy", getResp.StatusCode(), getResp.Body))
		return
	}

	state.Step = notifStepsRespToModel(*getResp.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleNotificationPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan scheduleNotificationPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
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

	scheduleID := plan.ScheduleID.ValueString()
	steps, d := r.putSteps(ctx, scheduleID, plan.Step)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := scheduleNotificationPolicyResourceModel{
		ID:         types.StringValue(scheduleID),
		ScheduleID: types.StringValue(scheduleID),
		Step:       steps,
		Timeouts:   plan.Timeouts,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleNotificationPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleNotificationPolicyResourceModel
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

	// There is no delete endpoint; clearing the policy is a PUT with an
	// empty step list (mirrors oncall_trigger_targets).
	_, d := r.putSteps(ctx, state.ScheduleID.ValueString(), nil)
	resp.Diagnostics.Append(d...)
}

func (r *scheduleNotificationPolicyResource) putSteps(ctx context.Context, scheduleID string, plan []notificationStepModel) ([]notificationStepModel, diag.Diagnostics) {
	inputs := make([]client.AdminNotifStepBody, len(plan))
	for i, s := range plan {
		pos := i
		inputs[i] = client.AdminNotifStepBody{
			Position:         &pos,
			StepType:         s.StepType.ValueStringPointer(),
			Tier:             intPtrFromInt64(s.Tier),
			UserId:           s.UserID.ValueStringPointer(),
			IntegrationId:    s.IntegrationID.ValueStringPointer(),
			RepeatCount:      intPtrFromInt64(s.RepeatCount),
			WaitAfterSeconds: intPtrFromInt64(s.WaitAfterSeconds),
		}
	}
	body, err := newBody((*client.PutAdminSchedulesIdNotificationPolicyJSONBody).FromPutAdminSchedulesIdNotificationPolicyJSONBody1, inputs)
	if err != nil {
		return nil, diagsFromError("Unable to encode notification policy request", err)
	}
	putResp, err := r.client.PutAdminSchedulesIdNotificationPolicyWithBodyWithResponse(ctx, scheduleID, "application/json", body)
	if err != nil {
		return nil, diagsFromError("Unable to set schedule notification policy", err)
	}
	if putResp.StatusCode() != http.StatusOK || putResp.JSON200 == nil {
		return nil, diagsFromDiagnostic(unexpectedStatus("Unable to set schedule notification policy", "PUT", "/admin/schedules/"+scheduleID+"/notification-policy", putResp.StatusCode(), putResp.Body))
	}
	return notifStepsRespToModel(*putResp.JSON200), nil
}

func notifStepsRespToModel(steps []client.AdminNotifStepResponse) []notificationStepModel {
	out := make([]notificationStepModel, len(steps))
	for i, s := range steps {
		out[i] = notificationStepModel{
			StepType:         strFromPtr(s.StepType),
			Tier:             int64FromIntPtr(s.Tier),
			UserID:           strFromPtr(s.UserId),
			IntegrationID:    strFromPtr(s.IntegrationId),
			RepeatCount:      int64FromIntPtr(s.RepeatCount),
			WaitAfterSeconds: int64FromIntPtr(s.WaitAfterSeconds),
		}
	}
	return out
}
