package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

var (
	_ resource.Resource                = (*cronResource)(nil)
	_ resource.ResourceWithImportState = (*cronResource)(nil)
)

// NewCronScheduleResource manages a cron schedule (full CRUD incl. PUT).
func NewCronScheduleResource() resource.Resource { return &cronResource{} }

type cronResource struct{ base }

type cronModel struct {
	ID            types.String         `tfsdk:"id"`
	TenantID      types.String         `tfsdk:"tenant_id"`
	Namespace     types.String         `tfsdk:"namespace"`
	SequenceID    types.String         `tfsdk:"sequence_id"`
	CronExpr      types.String         `tfsdk:"cron_expr"`
	Timezone      types.String         `tfsdk:"timezone"`
	Enabled       types.Bool           `tfsdk:"enabled"`
	Metadata      jsontypes.Normalized `tfsdk:"metadata"`
	OverlapPolicy types.String         `tfsdk:"overlap_policy"`
	NextFireAt    types.String         `tfsdk:"next_fire_at"`
	CreatedAt     types.String         `tfsdk:"created_at"`
	UpdatedAt     types.String         `tfsdk:"updated_at"`
}

func (r *cronResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cron_schedule"
}

func (r *cronResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A cron schedule that starts instances of a sequence version (`/cron`). `cron_expr`, `timezone`, " +
			"`enabled`, `metadata` and `overlap_policy` update in place (`PUT /cron/{id}`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Schedule UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"tenant_id": schema.StringAttribute{Optional: true, Computed: true,
				Description:   "Tenant. Defaults to the provider's tenant_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
			"namespace": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("default"),
				Description: "Namespace for created instances. Defaults to `default`.", PlanModifiers: replace},
			"sequence_id": schema.StringAttribute{Required: true,
				Description: "UUID of the sequence version to run (e.g. `orch8_sequence.x.id`).", PlanModifiers: replace},
			"cron_expr": schema.StringAttribute{Required: true,
				Description: "Cron expression, e.g. `0 0 9 * * MON-FRI` (validated by the engine)."},
			"timezone": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("UTC"),
				Description: "IANA timezone. Defaults to `UTC`."},
			"enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the schedule fires. Defaults to true."},
			"metadata": schema.StringAttribute{CustomType: jsontypes.NormalizedType{}, Optional: true,
				Description: "JSON object merged into the metadata of created instances."},
			"overlap_policy": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("allow"),
				Description: "`allow` (default), `skip`, or `buffer_one`.",
				Validators:  []validator.String{stringvalidator.OneOf("allow", "skip", "buffer_one")}},
			"next_fire_at": schema.StringAttribute{Computed: true, Description: "Next scheduled fire time."},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"updated_at": schema.StringAttribute{Computed: true, Description: "Last update timestamp."},
		},
	}
}

func (r *cronResource) fromWire(c *client.CronSchedule, m *cronModel) {
	m.ID = types.StringValue(c.ID)
	m.TenantID = types.StringValue(c.TenantID)
	m.Namespace = types.StringValue(c.Namespace)
	m.SequenceID = types.StringValue(c.SequenceID)
	m.CronExpr = types.StringValue(c.CronExpr)
	m.Timezone = types.StringValue(c.Timezone)
	m.Enabled = types.BoolValue(c.Enabled)
	m.Metadata = jsonFromServer(c.Metadata, m.Metadata)
	m.OverlapPolicy = types.StringValue(c.OverlapPolicy)
	m.NextFireAt = stringOrNull(c.NextFireAt)
	m.CreatedAt = types.StringValue(c.CreatedAt)
	m.UpdatedAt = types.StringValue(c.UpdatedAt)
}

func (r *cronResource) get(ctx context.Context, id string) (*client.CronSchedule, error) {
	var c client.CronSchedule
	err := r.client.Do(ctx, "GET", "/cron/"+client.PathEscape(id), nil, nil, &c)
	return &c, err
}

func (r *cronResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := r.tenantFor(plan.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	body := client.CreateCronRequest{
		TenantID:      tenant,
		Namespace:     plan.Namespace.ValueString(),
		SequenceID:    plan.SequenceID.ValueString(),
		CronExpr:      plan.CronExpr.ValueString(),
		Timezone:      plan.Timezone.ValueString(),
		Metadata:      rawJSON(plan.Metadata),
		Enabled:       plan.Enabled.ValueBool(),
		OverlapPolicy: plan.OverlapPolicy.ValueString(),
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := r.client.Do(ctx, "POST", "/cron", nil, body, &created); err != nil {
		addAPIError(&resp.Diagnostics, "create cron schedule", err)
		return
	}
	c, err := r.get(ctx, created.ID)
	if err != nil {
		addAPIError(&resp.Diagnostics, "read cron schedule "+created.ID+" after create", err)
		return
	}
	r.fromWire(c, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cronResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var st cronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.get(ctx, st.ID.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read cron schedule", err)
		return
	}
	r.fromWire(c, &st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &st)...)
}

func (r *cronResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, st cronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enabled := plan.Enabled.ValueBool()
	body := client.UpdateCronRequest{
		CronExpr:      strPtr(plan.CronExpr),
		Timezone:      strPtr(plan.Timezone),
		Enabled:       &enabled,
		OverlapPolicy: strPtr(plan.OverlapPolicy),
		Metadata:      rawJSON(plan.Metadata),
	}
	if body.Metadata == nil && !st.Metadata.IsNull() {
		body.Metadata = json.RawMessage(`{}`) // metadata removed from config: clear it
	}
	if err := r.client.Do(ctx, "PUT", "/cron/"+client.PathEscape(st.ID.ValueString()), nil, body, nil); err != nil {
		addAPIError(&resp.Diagnostics, "update cron schedule", err)
		return
	}
	// Re-read: the PUT response carries the pre-write updated_at.
	c, err := r.get(ctx, st.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "read cron schedule after update", err)
		return
	}
	r.fromWire(c, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cronResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var st cronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, "DELETE", "/cron/"+client.PathEscape(st.ID.ValueString()), nil, nil, nil)
	if err != nil && !isNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete cron schedule", err)
	}
}

func (r *cronResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
