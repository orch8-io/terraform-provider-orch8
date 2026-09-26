package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

var (
	_ resource.Resource                = (*rollbackPolicyResource)(nil)
	_ resource.ResourceWithImportState = (*rollbackPolicyResource)(nil)
)

// NewRollbackPolicyResource manages an automatic rollback policy.
func NewRollbackPolicyResource() resource.Resource { return &rollbackPolicyResource{} }

type rollbackPolicyResource struct{ base }

type rollbackPolicyModel struct {
	ID                     types.String  `tfsdk:"id"`
	PolicyID               types.Int64   `tfsdk:"policy_id"`
	TenantID               types.String  `tfsdk:"tenant_id"`
	SequenceName           types.String  `tfsdk:"sequence_name"`
	ErrorRateThreshold     types.Float64 `tfsdk:"error_rate_threshold"`
	TimeWindowSecs         types.Int64   `tfsdk:"time_window_secs"`
	CooldownSecs           types.Int64   `tfsdk:"cooldown_secs"`
	ConfirmationWindowSecs types.Int64   `tfsdk:"confirmation_window_secs"`
	WebhookURL             types.String  `tfsdk:"webhook_url"`
	Enabled                types.Bool    `tfsdk:"enabled"`
	CreatedAt              types.String  `tfsdk:"created_at"`
}

func (r *rollbackPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rollback_policy"
}

func (r *rollbackPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	window := []validator.Int64{int64validator.Between(0, 86400)}
	resp.Schema = schema.Schema{
		Description: "Automatic rollback policy for a sequence (`/rollback-policies`), keyed by tenant + sequence name. " +
			"`POST /rollback-policies` upserts, so thresholds update in place (and re-enable the policy).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "`<tenant_id>/<sequence_name>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"policy_id": schema.Int64Attribute{Computed: true, Description: "Engine row id.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"tenant_id": schema.StringAttribute{Optional: true, Computed: true,
				Description:   "Tenant. Defaults to the provider's tenant_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
			"sequence_name": schema.StringAttribute{Required: true, Description: "Sequence the policy watches.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"error_rate_threshold": schema.Float64Attribute{Required: true,
				Description: "Error rate in [0, 1] that triggers a rollback.",
				Validators:  []validator.Float64{float64validator.Between(0, 1)}},
			"time_window_secs": schema.Int64Attribute{Required: true, Description: "Evaluation window in seconds (> 0).",
				Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"cooldown_secs": schema.Int64Attribute{Optional: true, Computed: true,
				Description: "Minimum seconds between rollbacks (0–86400, engine default 3600).", Validators: window},
			"confirmation_window_secs": schema.Int64Attribute{Optional: true, Computed: true,
				Description: "Seconds the breach must persist (0–86400, engine default 60).", Validators: window},
			"webhook_url": schema.StringAttribute{Optional: true,
				Description: "Public URL to POST alerts to when a rollback triggers."},
			"enabled":    schema.BoolAttribute{Computed: true, Description: "Whether the policy is enabled."},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *rollbackPolicyResource) fromWire(w *client.RollbackPolicy, m *rollbackPolicyModel) {
	m.ID = types.StringValue(w.TenantID + "/" + w.SequenceName)
	m.PolicyID = types.Int64Value(w.ID)
	m.TenantID = types.StringValue(w.TenantID)
	m.SequenceName = types.StringValue(w.SequenceName)
	m.ErrorRateThreshold = types.Float64Value(w.ErrorRateThreshold)
	m.TimeWindowSecs = types.Int64Value(w.TimeWindowSecs)
	m.CooldownSecs = types.Int64Value(w.CooldownSecs)
	m.ConfirmationWindowSecs = types.Int64Value(w.ConfirmationWindowSecs)
	m.WebhookURL = stringOrNull(w.WebhookURL)
	m.Enabled = types.BoolValue(w.Enabled)
	m.CreatedAt = types.StringValue(w.CreatedAt)
}

func (r *rollbackPolicyResource) upsert(ctx context.Context, plan *rollbackPolicyModel, tenant string) (*client.RollbackPolicy, error) {
	body := client.CreateRollbackPolicyRequest{
		TenantID:               tenant,
		SequenceName:           plan.SequenceName.ValueString(),
		ErrorRateThreshold:     plan.ErrorRateThreshold.ValueFloat64(),
		TimeWindowSecs:         plan.TimeWindowSecs.ValueInt64(),
		CooldownSecs:           int64Ptr(plan.CooldownSecs),
		ConfirmationWindowSecs: int64Ptr(plan.ConfirmationWindowSecs),
		WebhookURL:             strPtr(plan.WebhookURL),
	}
	var w client.RollbackPolicy
	err := r.client.Do(ctx, "POST", "/rollback-policies", nil, body, &w)
	return &w, err
}

func (r *rollbackPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan rollbackPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := r.tenantFor(plan.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	w, err := r.upsert(ctx, &plan, tenant)
	if err != nil {
		addAPIError(&resp.Diagnostics, "create rollback policy", err)
		return
	}
	r.fromWire(w, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *rollbackPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var st rollbackPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w client.RollbackPolicy
	err := r.client.Do(ctx, "GET", "/rollback-policies/"+client.PathEscape(st.SequenceName.ValueString()),
		url.Values{"tenant_id": {st.TenantID.ValueString()}}, nil, &w)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read rollback policy", err)
		return
	}
	r.fromWire(&w, &st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &st)...)
}

func (r *rollbackPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, st rollbackPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := r.upsert(ctx, &plan, st.TenantID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "update rollback policy", err)
		return
	}
	r.fromWire(w, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *rollbackPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var st rollbackPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, "DELETE", "/rollback-policies/"+client.PathEscape(st.SequenceName.ValueString()),
		url.Values{"tenant_id": {st.TenantID.ValueString()}}, nil, nil)
	if err != nil && !isNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete rollback policy", err)
	}
}

// ImportState accepts "<tenant_id>/<sequence_name>".
func (r *rollbackPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error()+" (expected <tenant_id>/<sequence_name>)")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tenant_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("sequence_name"), parts[1])...)
}
