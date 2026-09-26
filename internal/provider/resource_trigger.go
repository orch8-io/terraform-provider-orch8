package provider

import (
	"context"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

var (
	_ resource.Resource                = (*triggerResource)(nil)
	_ resource.ResourceWithImportState = (*triggerResource)(nil)
)

// NewTriggerResource manages an inbound trigger (webhook/event/nats/...).
func NewTriggerResource() resource.Resource { return &triggerResource{} }

type triggerResource struct{ base }

type triggerModel struct {
	ID           types.String         `tfsdk:"id"`
	Slug         types.String         `tfsdk:"slug"`
	TenantID     types.String         `tfsdk:"tenant_id"`
	SequenceName types.String         `tfsdk:"sequence_name"`
	Namespace    types.String         `tfsdk:"namespace"`
	Version      types.Int64          `tfsdk:"version"`
	TriggerType  types.String         `tfsdk:"trigger_type"`
	Config       jsontypes.Normalized `tfsdk:"config"`
	Secret       types.String         `tfsdk:"secret"`
	Enabled      types.Bool           `tfsdk:"enabled"`
	CreatedAt    types.String         `tfsdk:"created_at"`
	UpdatedAt    types.String         `tfsdk:"updated_at"`
}

// replaceUnlessPinned: the engine can retarget a trigger in place
// (PATCH /triggers/{slug}/target) only to a concrete sequence version, so a
// change of target without a pinned `version` needs replacement.
func replaceUnlessPinnedString() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			var v types.Int64
			resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("version"), &v)...)
			resp.RequiresReplace = v.IsNull()
		},
		"Replaces the trigger when its target changes and `version` is not pinned.",
		"Replaces the trigger when its target changes and `version` is not pinned.",
	)
}

func (r *triggerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_trigger"
}

func (r *triggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "An Orch8 trigger that starts instances of a sequence (`POST /triggers`). Changing the target " +
			"sequence with a pinned `version` retargets in place (`PATCH /triggers/{slug}/target`); every other change replaces the trigger.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Same as `slug`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"slug": schema.StringAttribute{
				Required: true, Description: "Unique trigger slug (used in `/webhooks/{slug}` and `/triggers/{slug}/fire`).",
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"tenant_id": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Tenant. Defaults to the provider's tenant_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"sequence_name": schema.StringAttribute{
				Required: true, Description: "Name of the sequence to start.",
				PlanModifiers: []planmodifier.String{replaceUnlessPinnedString()},
			},
			"namespace": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("default"),
				Description:   "Namespace of the target sequence. Defaults to `default`.",
				PlanModifiers: []planmodifier.String{replaceUnlessPinnedString()},
			},
			"version": schema.Int64Attribute{
				Optional:    true,
				Description: "Pin a sequence version. Omit to always use the latest version.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = req.PlanValue.IsNull()
					}, "Replaces the trigger when the pin is removed.", "Replaces the trigger when the pin is removed.")},
			},
			"trigger_type": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("webhook"),
				Description:   "One of `webhook`, `nats`, `file_watch`, `event`, `activepieces_poll`. Defaults to `webhook`.",
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.OneOf("webhook", "nats", "file_watch", "event", "activepieces_poll")},
			},
			"config": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{}, Optional: true,
				Description:   "Type-specific configuration as a JSON object (e.g. NATS subject, file path, activepieces_poll settings).",
				PlanModifiers: replace,
			},
			"secret": schema.StringAttribute{
				Optional: true, Sensitive: true,
				Description: "Shared secret callers must present (`x-trigger-secret` / HMAC). Write-only: the engine never " +
					"returns it, so out-of-band changes are not detected. Changing it replaces the trigger.",
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"enabled":    schema.BoolAttribute{Computed: true, Description: "Whether the trigger is enabled."},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"updated_at": schema.StringAttribute{Computed: true, Description: "Last update timestamp (used for optimistic concurrency on retarget)."},
		},
	}
}

func (r *triggerResource) fromWire(t *client.Trigger, m *triggerModel) {
	m.ID = types.StringValue(t.Slug)
	m.Slug = types.StringValue(t.Slug)
	m.TenantID = types.StringValue(t.TenantID)
	m.SequenceName = types.StringValue(t.SequenceName)
	m.Namespace = types.StringValue(t.Namespace)
	m.Version = int64OrNull(t.Version)
	m.TriggerType = types.StringValue(t.TriggerType)
	m.Config = jsonFromServer(t.Config, m.Config)
	m.Enabled = types.BoolValue(t.Enabled)
	m.CreatedAt = types.StringValue(t.CreatedAt)
	m.UpdatedAt = types.StringValue(t.UpdatedAt)
}

func (r *triggerResource) get(ctx context.Context, slug string) (*client.Trigger, error) {
	var t client.Trigger
	err := r.client.Do(ctx, "GET", "/triggers/"+client.PathEscape(slug), nil, nil, &t)
	return &t, err
}

func (r *triggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan triggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := r.tenantFor(plan.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	body := client.CreateTriggerRequest{
		Slug:         plan.Slug.ValueString(),
		SequenceName: plan.SequenceName.ValueString(),
		Version:      int64Ptr(plan.Version),
		TenantID:     tenant,
		Namespace:    plan.Namespace.ValueString(),
		Secret:       strPtr(plan.Secret),
		TriggerType:  plan.TriggerType.ValueString(),
		Config:       rawJSON(plan.Config),
	}
	var t client.Trigger
	if err := r.client.Do(ctx, "POST", "/triggers", nil, body, &t); err != nil {
		addAPIError(&resp.Diagnostics, "create trigger", err)
		return
	}
	r.fromWire(&t, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *triggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var st triggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.get(ctx, st.Slug.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read trigger", err)
		return
	}
	r.fromWire(t, &st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &st)...)
}

func (r *triggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan triggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Version.IsNull() {
		resp.Diagnostics.AddError("Unexpected in-place update", "retargeting requires a pinned version")
		return
	}
	seqID := r.resolveSequenceID(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	cur, err := r.get(ctx, plan.Slug.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "read trigger before retarget", err)
		return
	}
	body := client.RetargetTriggerRequest{SequenceID: seqID, ExpectedUpdatedAt: cur.UpdatedAt}
	if err := r.client.Do(ctx, "PATCH", "/triggers/"+client.PathEscape(plan.Slug.ValueString())+"/target", nil, body, nil); err != nil {
		addAPIError(&resp.Diagnostics, "retarget trigger", err)
		return
	}
	t, err := r.get(ctx, plan.Slug.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "read trigger after retarget", err)
		return
	}
	r.fromWire(t, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *triggerResource) resolveSequenceID(ctx context.Context, m triggerModel, diags *diag.Diagnostics) string {
	q := url.Values{}
	q.Set("tenant_id", r.tenantFor(m.TenantID))
	q.Set("namespace", m.Namespace.ValueString())
	q.Set("name", m.SequenceName.ValueString())
	q.Set("version", strconv.FormatInt(m.Version.ValueInt64(), 10))
	var s struct {
		ID string `json:"id"`
	}
	if err := r.client.Do(ctx, "GET", "/sequences/by-name", q, nil, &s); err != nil {
		addAPIError(diags, "resolve target sequence", err)
		return ""
	}
	return s.ID
}

func (r *triggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var st triggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, "DELETE", "/triggers/"+client.PathEscape(st.Slug.ValueString()), nil, nil, nil)
	if err != nil && !isNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete trigger", err)
	}
}

func (r *triggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("slug"), req, resp)
}
