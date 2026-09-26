package provider

import (
	"context"

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
	_ resource.Resource                = (*credentialResource)(nil)
	_ resource.ResourceWithImportState = (*credentialResource)(nil)
)

// NewCredentialResource manages a stored credential (secret value is write-only).
func NewCredentialResource() resource.Resource { return &credentialResource{} }

type credentialResource struct{ base }

type credentialModel struct {
	ID              types.String `tfsdk:"id"`
	TenantID        types.String `tfsdk:"tenant_id"`
	Name            types.String `tfsdk:"name"`
	Kind            types.String `tfsdk:"kind"`
	Value           types.String `tfsdk:"value"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	RefreshURL      types.String `tfsdk:"refresh_url"`
	RefreshToken    types.String `tfsdk:"refresh_token"`
	Description     types.String `tfsdk:"description"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	HasRefreshToken types.Bool   `tfsdk:"has_refresh_token"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func (r *credentialResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_credential"
}

func (r *credentialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	clearReplace := []planmodifier.String{requiresReplaceWhenCleared()}
	resp.Schema = schema.Schema{
		Description: "A credential stored (encrypted) in the engine (`/credentials`) and referenced from sequences as " +
			"`credentials://<id>`. `value` and `refresh_token` are write-only: the engine never returns them, so " +
			"out-of-band changes are not detected, and after `terraform import` the next apply re-sends `value`. " +
			"Updates use `PATCH /credentials/{id}`; removing an optional field forces replacement because PATCH cannot clear it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Required: true,
				Description:   "Credential id (letters, digits, `-`, `_`, `.`; max 255).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 255)}},
			"tenant_id": schema.StringAttribute{Optional: true, Computed: true,
				Description:   "Tenant. Defaults to the provider's tenant_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "Display name."},
			"kind": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("api_key"),
				Description: "`api_key` (default), `oauth2`, or `basic`.",
				Validators:  []validator.String{stringvalidator.OneOf("api_key", "oauth2", "basic")}},
			"value": schema.StringAttribute{Required: true, Sensitive: true,
				Description: "Secret value (max 256 KiB). Write-only."},
			"expires_at":  schema.StringAttribute{Optional: true, Description: "RFC 3339 expiry (OAuth2 access tokens).", PlanModifiers: clearReplace},
			"refresh_url": schema.StringAttribute{Optional: true, Description: "OAuth2 token refresh URL.", PlanModifiers: clearReplace},
			"refresh_token": schema.StringAttribute{Optional: true, Sensitive: true,
				Description: "OAuth2 refresh token. Write-only.", PlanModifiers: clearReplace},
			"description": schema.StringAttribute{Optional: true, Description: "Free-form description.", PlanModifiers: clearReplace},
			"enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the credential can be resolved. Defaults to true."},
			"has_refresh_token": schema.BoolAttribute{Computed: true, Description: "Whether a refresh token is stored."},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"updated_at": schema.StringAttribute{Computed: true, Description: "Last update timestamp."},
		},
	}
}

func (r *credentialResource) fromWire(w *client.Credential, m *credentialModel) {
	m.ID = types.StringValue(w.ID)
	m.TenantID = types.StringValue(w.TenantID)
	m.Name = types.StringValue(w.Name)
	m.Kind = types.StringValue(w.Kind)
	m.Enabled = types.BoolValue(w.Enabled)
	m.ExpiresAt = keepInstant(m.ExpiresAt, w.ExpiresAt)
	m.RefreshURL = stringOrNull(w.RefreshURL)
	m.Description = stringOrNull(w.Description)
	m.HasRefreshToken = types.BoolValue(w.HasRefreshToken)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.UpdatedAt = types.StringValue(w.UpdatedAt)
}

func (r *credentialResource) path(id string) string { return "/credentials/" + client.PathEscape(id) }

func (r *credentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan credentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := r.tenantFor(plan.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	body := client.CreateCredentialRequest{
		ID:           plan.ID.ValueString(),
		Name:         plan.Name.ValueString(),
		Kind:         plan.Kind.ValueString(),
		Value:        plan.Value.ValueString(),
		TenantID:     tenant,
		ExpiresAt:    strPtr(plan.ExpiresAt),
		RefreshURL:   strPtr(plan.RefreshURL),
		RefreshToken: strPtr(plan.RefreshToken),
		Description:  strPtr(plan.Description),
	}
	var w client.Credential
	if err := r.client.Do(ctx, "POST", "/credentials", nil, body, &w); err != nil {
		addAPIError(&resp.Diagnostics, "create credential", err)
		return
	}
	// The create endpoint has no `enabled` field; disable via PATCH.
	if !plan.Enabled.ValueBool() && w.Enabled {
		f := false
		if err := r.client.Do(ctx, "PATCH", r.path(w.ID), nil, client.UpdateCredentialRequest{Enabled: &f}, &w); err != nil {
			addAPIError(&resp.Diagnostics, "disable credential", err)
		}
	}
	r.fromWire(&w, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *credentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var st credentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w client.Credential
	err := r.client.Do(ctx, "GET", r.path(st.ID.ValueString()), nil, nil, &w)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read credential", err)
		return
	}
	r.fromWire(&w, &st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &st)...)
}

func changed(plan, state types.String) *string {
	if plan.Equal(state) {
		return nil
	}
	return strPtr(plan)
}

func (r *credentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, st credentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := client.UpdateCredentialRequest{
		Name:         changed(plan.Name, st.Name),
		Kind:         changed(plan.Kind, st.Kind),
		Value:        changed(plan.Value, st.Value),
		ExpiresAt:    changed(plan.ExpiresAt, st.ExpiresAt),
		RefreshURL:   changed(plan.RefreshURL, st.RefreshURL),
		RefreshToken: changed(plan.RefreshToken, st.RefreshToken),
		Description:  changed(plan.Description, st.Description),
	}
	if !plan.Enabled.Equal(st.Enabled) {
		e := plan.Enabled.ValueBool()
		body.Enabled = &e
	}
	var w client.Credential
	if err := r.client.Do(ctx, "PATCH", r.path(st.ID.ValueString()), nil, body, &w); err != nil {
		addAPIError(&resp.Diagnostics, "update credential", err)
		return
	}
	r.fromWire(&w, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *credentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var st credentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Do(ctx, "DELETE", r.path(st.ID.ValueString()), nil, nil, nil); err != nil && !isNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete credential", err)
	}
}

func (r *credentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
