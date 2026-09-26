package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

var (
	_ resource.Resource                = (*queueRouteResource)(nil)
	_ resource.ResourceWithImportState = (*queueRouteResource)(nil)
)

// NewQueueRouteResource manages a queue routing rule (/routing-rules).
func NewQueueRouteResource() resource.Resource { return &queueRouteResource{} }

type queueRouteResource struct{ base }

type queueRouteModel struct {
	ID            types.String `tfsdk:"id"`
	TenantID      types.String `tfsdk:"tenant_id"`
	HandlerName   types.String `tfsdk:"handler_name"`
	MatchQueue    types.String `tfsdk:"match_queue"`
	QueueOverride types.String `tfsdk:"queue_override"`
	Priority      types.Int64  `tfsdk:"priority"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func (r *queueRouteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_queue_route"
}

func (r *queueRouteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A queue routing rule (`/routing-rules`): tasks for `handler_name` (optionally only those enqueued on " +
			"`match_queue`) are redirected to `queue_override`. The API has no update, so every change replaces the rule.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Rule UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"tenant_id": schema.StringAttribute{Optional: true, Computed: true,
				Description:   "Tenant. Defaults to the provider's tenant_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
			"handler_name": schema.StringAttribute{Required: true, Description: "Handler whose tasks are routed.", PlanModifiers: replace},
			"match_queue": schema.StringAttribute{Optional: true,
				Description: "Only route tasks originally destined for this queue. Omit to match any queue.", PlanModifiers: replace},
			"queue_override": schema.StringAttribute{Required: true, Description: "Destination queue.", PlanModifiers: replace},
			"priority": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				Description: "Rule precedence; higher wins. Defaults to 0.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the rule is active. Defaults to true.", PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *queueRouteResource) fromWire(w *client.RoutingRule, m *queueRouteModel) {
	m.ID = types.StringValue(w.ID)
	m.TenantID = types.StringValue(w.TenantID)
	m.HandlerName = types.StringValue(w.HandlerName)
	m.MatchQueue = stringOrNull(w.MatchQueue)
	m.QueueOverride = types.StringValue(w.QueueOverride)
	m.Priority = types.Int64Value(w.Priority)
	m.Enabled = types.BoolValue(w.Enabled)
	m.CreatedAt = types.StringValue(w.CreatedAt)
}

func (r *queueRouteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan queueRouteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := r.tenantFor(plan.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	body := client.RoutingRule{
		TenantID:      tenant,
		HandlerName:   plan.HandlerName.ValueString(),
		MatchQueue:    strPtr(plan.MatchQueue),
		QueueOverride: plan.QueueOverride.ValueString(),
		Priority:      plan.Priority.ValueInt64(),
		Enabled:       plan.Enabled.ValueBool(),
	}
	var w client.RoutingRule
	if err := r.client.Do(ctx, "POST", "/routing-rules", nil, body, &w); err != nil {
		addAPIError(&resp.Diagnostics, "create routing rule", err)
		return
	}
	r.fromWire(&w, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *queueRouteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var st queueRouteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w client.RoutingRule
	err := r.client.Do(ctx, "GET", "/routing-rules/"+client.PathEscape(st.ID.ValueString()), nil, nil, &w)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read routing rule", err)
		return
	}
	r.fromWire(&w, &st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &st)...)
}

func (r *queueRouteResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "orch8_queue_route has no update API; every change forces replacement.")
}

func (r *queueRouteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var st queueRouteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, "DELETE", "/routing-rules/"+client.PathEscape(st.ID.ValueString()), nil, nil, nil)
	if err != nil && !isNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete routing rule", err)
	}
}

func (r *queueRouteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
