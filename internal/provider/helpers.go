package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

// base is embedded by every resource and data source to receive the client.
type base struct {
	client *client.Client
}

func (b *base) configure(data any, diags *diag.Diagnostics) {
	if data == nil {
		return
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", data))
		return
	}
	b.client = c
}

func (b *base) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	b.configure(req.ProviderData, &resp.Diagnostics)
}

type dsBase struct{ base }

func (b *dsBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	b.configure(req.ProviderData, &resp.Diagnostics)
}

// tenantFor resolves a resource's tenant: the explicit attribute, else the
// provider's tenant. Returns "" when neither is set.
func (b *base) tenantFor(v types.String) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	if b.client != nil {
		return b.client.TenantID
	}
	return ""
}

func requireTenant(tenant string, diags *diag.Diagnostics) bool {
	if tenant == "" {
		diags.AddError("Missing tenant_id",
			"Set tenant_id on the resource or on the provider (or ORCH8_TENANT_ID).")
		return false
	}
	return true
}

// strPtr returns nil for null/unknown/empty-unknown values.
func strPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func stringOrNull(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func int64OrNull(p *int64) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

// rawJSON converts a jsontypes.Normalized to a RawMessage (nil when null).
func rawJSON(v jsontypes.Normalized) json.RawMessage {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return json.RawMessage(v.ValueString())
}

// isEmptyJSON is true for absent, null, or {} documents.
func isEmptyJSON(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		return true
	}
	var m map[string]any
	if json.Unmarshal(t, &m) == nil && len(m) == 0 {
		return true
	}
	return false
}

// jsonFromServer maps a server JSON document into state. When the server
// holds an "empty" document and the prior value is null, null is kept so
// omitting the attribute doesn't produce a perpetual diff.
func jsonFromServer(raw json.RawMessage, prior jsontypes.Normalized) jsontypes.Normalized {
	if isEmptyJSON(raw) {
		if prior.IsNull() || prior.IsUnknown() {
			return jsontypes.NewNormalizedNull()
		}
		if isEmptyJSON(json.RawMessage(prior.ValueString())) {
			return prior
		}
		if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
			return jsontypes.NewNormalizedNull()
		}
	}
	return jsontypes.NewNormalizedValue(string(raw))
}

// splitImportID splits "a/b" import identifiers.
func splitImportID(id string, parts int) ([]string, error) {
	s := strings.SplitN(id, "/", parts)
	if len(s) != parts {
		return nil, fmt.Errorf("expected import ID with %d '/'-separated parts, got %q", parts, id)
	}
	for _, p := range s {
		if p == "" {
			return nil, fmt.Errorf("import ID %q has an empty part", id)
		}
	}
	return s, nil
}

// requiresReplaceWhenCleared forces replacement when an optional attribute
// goes from set to null — for PATCH/PUT APIs that cannot clear a field.
func requiresReplaceWhenCleared() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull() && req.PlanValue.IsNull()
		},
		"Replaces the resource when the value is removed, because the engine API cannot clear it in place.",
		"Replaces the resource when the value is removed, because the engine API cannot clear it in place.",
	)
}

func addAPIError(diags *diag.Diagnostics, action string, err error) {
	diags.AddError("Orch8 API error", fmt.Sprintf("Unable to %s: %s", action, err))
}

func isNotFound(err error) bool { return client.IsNotFound(err) }

// sameInstant reports whether two RFC 3339 strings denote the same instant
// (the engine may re-render timestamps with a different precision/offset).
func sameInstant(a, b string) bool {
	if a == b {
		return true
	}
	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	return errA == nil && errB == nil && ta.Equal(tb)
}

// keepInstant returns the prior value when it denotes the same instant as
// the server value, else the server value (null when nil).
func keepInstant(prior types.String, server *string) types.String {
	if server == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() && sameInstant(prior.ValueString(), *server) {
		return prior
	}
	return types.StringValue(*server)
}
