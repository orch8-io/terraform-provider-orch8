package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

var (
	_ resource.Resource                = (*apiKeyResource)(nil)
	_ resource.ResourceWithImportState = (*apiKeyResource)(nil)
)

// NewAPIKeyResource manages a capability-scoped tenant API key.
func NewAPIKeyResource() resource.Resource { return &apiKeyResource{} }

type apiKeyResource struct{ base }

type apiKeyModel struct {
	ID           types.String `tfsdk:"id"`
	TenantID     types.String `tfsdk:"tenant_id"`
	Name         types.String `tfsdk:"name"`
	Capabilities types.Set    `tfsdk:"capabilities"`
	ExpiresAt    types.String `tfsdk:"expires_at"`
	Secret       types.String `tfsdk:"secret"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

var apiCapabilities = []string{"operator", "worker", "device", "publisher", "approver", "auditor"}

func (r *apiKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A tenant API key (`/api-keys`). Requires the provider to use the engine's root API key. " +
			"Keys are immutable: any change revokes and re-mints. Destroy revokes the key (`DELETE /api-keys/{id}`); a key " +
			"revoked out of band disappears from state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Key id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"tenant_id": schema.StringAttribute{Optional: true, Computed: true,
				Description:   "Tenant the key is bound to. Defaults to the provider's tenant_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Description: "Human-readable label.", PlanModifiers: replace},
			"capabilities": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				Description:   "Any of `operator`, `worker`, `device`, `publisher`, `approver`, `auditor`. The engine defaults to `[\"operator\"]`.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace(), setplanmodifier.UseStateForUnknown()},
				Validators: []validator.Set{setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(apiCapabilities...))}},
			"expires_at": schema.StringAttribute{Optional: true,
				Description: "Optional RFC 3339 expiry.", PlanModifiers: replace},
			"secret": schema.StringAttribute{Computed: true, Sensitive: true,
				Description:   "The key secret. Only returned at creation (null after import).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *apiKeyResource) fromWire(ctx context.Context, k *client.APIKey, m *apiKeyModel, diags *diag.Diagnostics) {
	m.ID = types.StringValue(k.ID)
	m.TenantID = types.StringValue(k.TenantID)
	m.Name = types.StringValue(k.Name)
	caps, d := types.SetValueFrom(ctx, types.StringType, k.Capabilities)
	diags.Append(d...)
	m.Capabilities = caps
	m.ExpiresAt = keepInstant(m.ExpiresAt, k.ExpiresAt)
	m.CreatedAt = types.StringValue(k.CreatedAt)
}

func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apiKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := r.tenantFor(plan.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	body := client.CreateAPIKeyRequest{TenantID: tenant, Name: plan.Name.ValueString(), ExpiresAt: strPtr(plan.ExpiresAt)}
	if !plan.Capabilities.IsNull() && !plan.Capabilities.IsUnknown() {
		resp.Diagnostics.Append(plan.Capabilities.ElementsAs(ctx, &body.Capabilities, false)...)
	}
	var k client.APIKey
	if err := r.client.Do(ctx, "POST", "/api-keys", nil, body, &k); err != nil {
		addAPIError(&resp.Diagnostics, "create API key", err)
		return
	}
	r.fromWire(ctx, &k, &plan, &resp.Diagnostics)
	plan.Secret = types.StringValue(k.Secret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// find lists the tenant's keys (the API has no GET-by-id) and returns the
// live key with the given id, or nil if missing/revoked.
func (r *apiKeyResource) find(ctx context.Context, tenant, id string) (*client.APIKey, error) {
	var keys []client.APIKey
	if err := r.client.Do(ctx, "GET", "/api-keys", url.Values{"tenant_id": {tenant}}, nil, &keys); err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id && !keys[i].Revoked {
			return &keys[i], nil
		}
	}
	return nil, nil
}

func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var st apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k, err := r.find(ctx, st.TenantID.ValueString(), st.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "list API keys", err)
		return
	}
	if k == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fromWire(ctx, k, &st, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &st)...)
}

func (r *apiKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "orch8_api_key is immutable; every change forces replacement.")
}

func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var st apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, "DELETE", "/api-keys/"+client.PathEscape(st.ID.ValueString()), nil, nil, nil)
	if err != nil && !isNotFound(err) {
		addAPIError(&resp.Diagnostics, "revoke API key", err)
	}
}

// ImportState accepts "<tenant_id>/<key_id>".
func (r *apiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error()+" (expected <tenant_id>/<key_id>)")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tenant_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("capabilities"), types.SetNull(types.StringType))...)
}
