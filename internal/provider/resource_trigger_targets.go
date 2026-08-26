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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

var (
	_ resource.Resource                = &triggerTargetsResource{}
	_ resource.ResourceWithConfigure   = &triggerTargetsResource{}
	_ resource.ResourceWithImportState = &triggerTargetsResource{}
)

func newTriggerTargetsResource() resource.Resource {
	return &triggerTargetsResource{}
}

type triggerTargetsResource struct {
	client *client.ClientWithResponses
}

type triggerTargetModel struct {
	TargetType    types.String `tfsdk:"target_type"`
	OnEvent       types.String `tfsdk:"on_event"`
	ScheduleID    types.String `tfsdk:"schedule_id"`
	UserID        types.String `tfsdk:"user_id"`
	IntegrationID types.String `tfsdk:"integration_id"`
	UserName      types.String `tfsdk:"user_name"`
}

type triggerTargetsResourceModel struct {
	ID        types.String         `tfsdk:"id"`
	TriggerID types.String         `tfsdk:"trigger_id"`
	Target    []triggerTargetModel `tfsdk:"target"`
	Timeouts  timeouts.Value       `tfsdk:"timeouts"`
}

func (r *triggerTargetsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_trigger_targets"
}

func (r *triggerTargetsResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the full set of notification targets for an oncall_trigger. " +
			"This is a singleton per trigger: the target list is atomically replaced on every " +
			"apply. Destroying this resource clears all targets (PUT with an empty list).\n\n" +
			"The API tracks position independently within each on_event group and always " +
			"lists targets grouped as fired, then state_change, then webhook (see " +
			"tfprovider-07's implementation notes) — list target blocks in that same grouped " +
			"order to avoid a reorder diff on every plan after the first apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Same value as trigger_id; this resource is keyed 1:1 to a trigger.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"trigger_id": schema.StringAttribute{
				Required:      true,
				Description:   "Trigger these targets belong to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
		Blocks: map[string]schema.Block{
			"target": schema.ListNestedBlock{
				Description: "One notification target. Position within its on_event group is " +
					"derived from block order (grouped fired, state_change, webhook).",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"target_type": schema.StringAttribute{
							Required:    true,
							Description: "One of \"schedule\", \"user\", \"integration\".",
							Validators:  []validator.String{stringvalidator.OneOf("schedule", "user", "integration")},
						},
						"on_event": schema.StringAttribute{
							Required: true,
							Description: "One of \"fired\", \"state_change\", \"webhook\". " +
								"\"webhook\" only supports target_type=\"integration\".",
							Validators: []validator.String{stringvalidator.OneOf("fired", "state_change", "webhook")},
						},
						"schedule_id": schema.StringAttribute{
							Optional:    true,
							Description: "Required (and only valid) when target_type = \"schedule\". Must share a team with the trigger.",
						},
						"user_id": schema.StringAttribute{
							Optional:    true,
							Description: "Required (and only valid) when target_type = \"user\".",
						},
						"integration_id": schema.StringAttribute{
							Optional:    true,
							Description: "Required (and only valid) when target_type = \"integration\". Must share a team with the trigger.",
						},
						"user_name": schema.StringAttribute{
							Computed:    true,
							Description: "Display name when target_type = \"user\" (not the email, which is PII).",
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

func (r *triggerTargetsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *triggerTargetsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("trigger_id"), req, resp)
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *triggerTargetsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan triggerTargetsResourceModel
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

	triggerID := plan.TriggerID.ValueString()
	targets, d := r.putTargets(ctx, triggerID, plan.Target)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := triggerTargetsResourceModel{
		ID:        types.StringValue(triggerID),
		TriggerID: types.StringValue(triggerID),
		Target:    targets,
		Timeouts:  plan.Timeouts,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *triggerTargetsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state triggerTargetsResourceModel
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

	triggerID := state.TriggerID.ValueString()
	targets, notFound, d := r.getTargets(ctx, triggerID)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	if notFound {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Target = targets
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *triggerTargetsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan triggerTargetsResourceModel
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

	triggerID := plan.TriggerID.ValueString()
	targets, d := r.putTargets(ctx, triggerID, plan.Target)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := triggerTargetsResourceModel{
		ID:        types.StringValue(triggerID),
		TriggerID: types.StringValue(triggerID),
		Target:    targets,
		Timeouts:  plan.Timeouts,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *triggerTargetsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state triggerTargetsResourceModel
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

	// There is no delete endpoint; clearing the targets is a PUT with an
	// empty list (per tfprovider-07's plan table: "set empty").
	_, d := r.putTargets(ctx, state.TriggerID.ValueString(), nil)
	resp.Diagnostics.Append(d...)
}

// putTargets groups plan targets by on_event (preserving relative order
// within each group) and assigns each one a position that's a counter local
// to its group — position uniqueness in replaceTargets is enforced per
// on_event, not globally (internal/triggers/handler.go's validateTargets).
// The PUT itself returns 204 with no body, so the resulting state comes from
// a follow-up GET.
func (r *triggerTargetsResource) putTargets(ctx context.Context, triggerID string, plan []triggerTargetModel) ([]triggerTargetModel, diag.Diagnostics) {
	positions := map[string]int{}
	inputs := make([]client.TriggersTargetInput, len(plan))
	for i := range plan {
		t := &plan[i]
		onEvent := t.OnEvent.ValueString()
		pos := positions[onEvent]
		positions[onEvent] = pos + 1
		inputs[i] = client.TriggersTargetInput{
			TargetType:    t.TargetType.ValueStringPointer(),
			OnEvent:       t.OnEvent.ValueStringPointer(),
			Position:      &pos,
			ScheduleId:    t.ScheduleID.ValueStringPointer(),
			UserId:        t.UserID.ValueStringPointer(),
			IntegrationId: t.IntegrationID.ValueStringPointer(),
		}
	}
	body, err := newBody((*client.PutAdminTriggersIdTargetsJSONBody).FromTriggersReplaceTargetsBody, client.TriggersReplaceTargetsBody{
		Targets: &inputs,
	})
	if err != nil {
		return nil, diagsFromError("Unable to encode trigger targets request", err)
	}
	putResp, err := r.client.PutAdminTriggersIdTargetsWithBodyWithResponse(ctx, triggerID, "application/json", body)
	if err != nil {
		return nil, diagsFromError("Unable to set trigger targets", err)
	}
	if putResp.StatusCode() != http.StatusNoContent {
		return nil, diagsFromDiagnostic(unexpectedStatus("Unable to set trigger targets", "PUT", "/admin/triggers/"+triggerID+"/targets", putResp.StatusCode(), putResp.Body))
	}

	targets, notFound, d := r.getTargets(ctx, triggerID)
	if notFound {
		return nil, diagsFromError("Unable to read back trigger targets", fmt.Errorf("trigger %s not found immediately after PUT .../targets", triggerID))
	}
	return targets, d
}

func (r *triggerTargetsResource) getTargets(ctx context.Context, triggerID string) (targets []triggerTargetModel, notFound bool, diags diag.Diagnostics) {
	getResp, err := r.client.GetAdminTriggersIdTargetsWithResponse(ctx, triggerID)
	if err != nil {
		return nil, false, diagsFromError("Unable to read trigger targets", err)
	}
	if getResp.StatusCode() == http.StatusNotFound {
		return nil, true, nil
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		return nil, false, diagsFromDiagnostic(unexpectedStatus("Unable to read trigger targets", "GET", "/admin/triggers/"+triggerID+"/targets", getResp.StatusCode(), getResp.Body))
	}
	out := make([]triggerTargetModel, len(*getResp.JSON200))
	for i, t := range *getResp.JSON200 {
		out[i] = triggerTargetModel{
			TargetType:    strFromPtr(t.TargetType),
			OnEvent:       strFromPtr(t.OnEvent),
			ScheduleID:    strFromPtr(t.ScheduleId),
			UserID:        strFromPtr(t.UserId),
			IntegrationID: strFromPtr(t.IntegrationId),
			UserName:      strFromPtr(t.UserName),
		}
	}
	return out, false, nil
}
