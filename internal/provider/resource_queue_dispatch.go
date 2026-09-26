package provider

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

var (
	_ resource.Resource                = (*queueDispatchResource)(nil)
	_ resource.ResourceWithImportState = (*queueDispatchResource)(nil)
)

// NewQueueDispatchResource manages per-queue dispatch mode (/queues/dispatch).
func NewQueueDispatchResource() resource.Resource { return &queueDispatchResource{} }

type queueDispatchResource struct{ base }

type queueDispatchModel struct {
	ID        types.String `tfsdk:"id"`
	TenantID  types.String `tfsdk:"tenant_id"`
	QueueName types.String `tfsdk:"queue_name"`
	Mode      types.String `tfsdk:"mode"`
	PushURL   types.String `tfsdk:"push_url"`
	Secret    types.String `tfsdk:"secret"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func (r *queueDispatchResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_queue_dispatch"
}

func (r *queueDispatchResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Dispatch mode of a worker queue (`/queues/dispatch`): `poll` (workers poll) or `push` (the engine POSTs " +
			"a signed envelope to `push_url`). `POST /queues/dispatch` is an upsert, so mode/URL/secret update in place.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "`<tenant_id>/<queue_name>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"tenant_id": schema.StringAttribute{Optional: true, Computed: true,
				Description:   "Tenant. Defaults to the provider's tenant_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
			"queue_name": schema.StringAttribute{Required: true, Description: "Queue name.", PlanModifiers: replace},
			"mode": schema.StringAttribute{Required: true, Description: "`poll` or `push`.",
				Validators: []validator.String{stringvalidator.OneOf("poll", "push")}},
			"push_url": schema.StringAttribute{Optional: true,
				Description: "Public HTTPS URL for `push` mode (required when mode = push)."},
			"secret": schema.StringAttribute{Optional: true, Sensitive: true,
				Description: "HMAC secret for pushed envelopes. Write-only: never returned by the engine; removing it clears it."},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"updated_at": schema.StringAttribute{Computed: true, Description: "Last update timestamp."},
		},
	}
}

func (r *queueDispatchResource) fromWire(w *client.QueueDispatch, m *queueDispatchModel) {
	m.ID = types.StringValue(w.TenantID + "/" + w.QueueName)
	m.TenantID = types.StringValue(w.TenantID)
	m.QueueName = types.StringValue(w.QueueName)
	m.Mode = types.StringValue(w.Mode)
	m.PushURL = stringOrNull(w.PushURL)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.UpdatedAt = types.StringValue(w.UpdatedAt)
}

// upsert POSTs the config. The engine's `secret` field is tri-state:
// absent = keep, null = clear, string = set.
func (r *queueDispatchResource) upsert(ctx context.Context, plan *queueDispatchModel, prior *queueDispatchModel) (*client.QueueDispatch, error) {
	body := map[string]any{
		"tenant_id":  plan.TenantID.ValueString(),
		"queue_name": plan.QueueName.ValueString(),
		"mode":       plan.Mode.ValueString(),
	}
	if p := strPtr(plan.PushURL); p != nil {
		body["push_url"] = *p
	}
	switch {
	case !plan.Secret.IsNull() && !plan.Secret.IsUnknown():
		body["secret"] = plan.Secret.ValueString()
	case prior != nil && !prior.Secret.IsNull():
		body["secret"] = json.RawMessage("null")
	}
	var w client.QueueDispatch
	err := r.client.Do(ctx, "POST", "/queues/dispatch", nil, body, &w)
	return &w, err
}

func (r *queueDispatchResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan queueDispatchModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := r.tenantFor(plan.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	plan.TenantID = types.StringValue(tenant)
	w, err := r.upsert(ctx, &plan, nil)
	if err != nil {
		addAPIError(&resp.Diagnostics, "set queue dispatch", err)
		return
	}
	r.fromWire(w, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *queueDispatchResource) find(ctx context.Context, tenant, queue string) (*client.QueueDispatch, error) {
	var list []client.QueueDispatch
	if err := r.client.Do(ctx, "GET", "/queues/dispatch", url.Values{"tenant_id": {tenant}}, nil, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].QueueName == queue {
			return &list[i], nil
		}
	}
	return nil, nil
}

func (r *queueDispatchResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var st queueDispatchModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := r.find(ctx, st.TenantID.ValueString(), st.QueueName.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "list queue dispatch configs", err)
		return
	}
	if w == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fromWire(w, &st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &st)...)
}

func (r *queueDispatchResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, st queueDispatchModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.TenantID = st.TenantID
	w, err := r.upsert(ctx, &plan, &st)
	if err != nil {
		addAPIError(&resp.Diagnostics, "update queue dispatch", err)
		return
	}
	r.fromWire(w, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *queueDispatchResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var st queueDispatchModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := "/queues/dispatch/" + client.PathEscape(st.TenantID.ValueString()) + "/" + client.PathEscape(st.QueueName.ValueString())
	if err := r.client.Do(ctx, "DELETE", p, nil, nil, nil); err != nil && !isNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete queue dispatch", err)
	}
}

// ImportState accepts "<tenant_id>/<queue_name>".
func (r *queueDispatchResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error()+" (expected <tenant_id>/<queue_name>)")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tenant_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("queue_name"), parts[1])...)
}
