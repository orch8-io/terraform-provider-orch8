package provider

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

// ---------------------------------------------------------------- sequence

// NewSequenceDataSource looks up a sequence by id or by name.
func NewSequenceDataSource() datasource.DataSource { return &sequenceDataSource{} }

type sequenceDataSource struct{ dsBase }

type sequenceDSModel struct {
	ID         types.String         `tfsdk:"id"`
	TenantID   types.String         `tfsdk:"tenant_id"`
	Namespace  types.String         `tfsdk:"namespace"`
	Name       types.String         `tfsdk:"name"`
	Version    types.Int64          `tfsdk:"version"`
	Status     types.String         `tfsdk:"status"`
	Deprecated types.Bool           `tfsdk:"deprecated"`
	CreatedAt  types.String         `tfsdk:"created_at"`
	Definition jsontypes.Normalized `tfsdk:"definition"`
}

func (d *sequenceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sequence"
}

func (d *sequenceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a sequence version by `id` (`GET /sequences/{id}`) or by `name` (+ optional `namespace`, " +
			"`tenant_id`, `version`; `GET /sequences/by-name` — latest version when `version` is omitted).",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Optional: true, Computed: true, Description: "Sequence UUID. Set this or `name`."},
			"tenant_id":  schema.StringAttribute{Optional: true, Computed: true, Description: "Tenant (defaults to the provider's)."},
			"namespace":  schema.StringAttribute{Optional: true, Computed: true, Description: "Namespace (default `default`)."},
			"name":       schema.StringAttribute{Optional: true, Computed: true, Description: "Sequence name. Set this or `id`."},
			"version":    schema.Int64Attribute{Optional: true, Computed: true, Description: "Version; latest when omitted in a by-name lookup."},
			"status":     schema.StringAttribute{Computed: true, Description: "Lifecycle status."},
			"deprecated": schema.BoolAttribute{Computed: true, Description: "Whether the version is deprecated."},
			"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp."},
			"definition": schema.StringAttribute{CustomType: jsontypes.NormalizedType{}, Computed: true,
				Description: "Full sequence document as returned by the engine (JSON)."},
		},
	}
}

func (d *sequenceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sequenceDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var raw json.RawMessage
	var err error
	switch {
	case !cfg.ID.IsNull() && cfg.ID.ValueString() != "":
		err = d.client.Do(ctx, "GET", "/sequences/"+client.PathEscape(cfg.ID.ValueString()), nil, nil, &raw)
	case !cfg.Name.IsNull() && cfg.Name.ValueString() != "":
		tenant := d.tenantFor(cfg.TenantID)
		if !requireTenant(tenant, &resp.Diagnostics) {
			return
		}
		ns := cfg.Namespace.ValueString()
		if ns == "" {
			ns = "default"
		}
		q := url.Values{"tenant_id": {tenant}, "namespace": {ns}, "name": {cfg.Name.ValueString()}}
		if !cfg.Version.IsNull() {
			q.Set("version", strconv.FormatInt(cfg.Version.ValueInt64(), 10))
		}
		err = d.client.Do(ctx, "GET", "/sequences/by-name", q, nil, &raw)
	default:
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Missing lookup key", "Set either `id` or `name`.")
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "look up sequence", err)
		return
	}
	var s sequenceWire
	if err := json.Unmarshal(raw, &s); err != nil {
		resp.Diagnostics.AddError("Decode error", err.Error())
		return
	}
	cfg.ID = types.StringValue(s.ID)
	cfg.TenantID = types.StringValue(s.TenantID)
	cfg.Namespace = types.StringValue(s.Namespace)
	cfg.Name = types.StringValue(s.Name)
	cfg.Version = types.Int64Value(s.Version)
	cfg.Status = types.StringValue(s.Status)
	cfg.Deprecated = types.BoolValue(s.Deprecated)
	cfg.CreatedAt = types.StringValue(s.CreatedAt)
	cfg.Definition = jsontypes.NewNormalizedValue(string(raw))
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// ---------------------------------------------------------------- instance

// NewInstanceDataSource reads a workflow instance by id.
func NewInstanceDataSource() datasource.DataSource { return &instanceDataSource{} }

type instanceDataSource struct{ dsBase }

type instanceDSModel struct {
	ID               types.String         `tfsdk:"id"`
	SequenceID       types.String         `tfsdk:"sequence_id"`
	TenantID         types.String         `tfsdk:"tenant_id"`
	Namespace        types.String         `tfsdk:"namespace"`
	State            types.String         `tfsdk:"state"`
	Priority         types.String         `tfsdk:"priority"`
	Timezone         types.String         `tfsdk:"timezone"`
	NextFireAt       types.String         `tfsdk:"next_fire_at"`
	IdempotencyKey   types.String         `tfsdk:"idempotency_key"`
	ParentInstanceID types.String         `tfsdk:"parent_instance_id"`
	Metadata         jsontypes.Normalized `tfsdk:"metadata"`
	Context          jsontypes.Normalized `tfsdk:"context"`
	CreatedAt        types.String         `tfsdk:"created_at"`
	UpdatedAt        types.String         `tfsdk:"updated_at"`
}

type instanceWire struct {
	ID               string          `json:"id"`
	SequenceID       string          `json:"sequence_id"`
	TenantID         string          `json:"tenant_id"`
	Namespace        string          `json:"namespace"`
	State            string          `json:"state"`
	Priority         json.RawMessage `json:"priority"`
	Timezone         string          `json:"timezone"`
	NextFireAt       *string         `json:"next_fire_at"`
	IdempotencyKey   *string         `json:"idempotency_key"`
	ParentInstanceID *string         `json:"parent_instance_id"`
	Metadata         json.RawMessage `json:"metadata"`
	Context          json.RawMessage `json:"context"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
}

func (d *instanceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

func (d *instanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	c := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, Description: desc}
	}
	resp.Schema = schema.Schema{
		Description: "Read a workflow instance (`GET /instances/{id}`).",
		Attributes: map[string]schema.Attribute{
			"id":                 schema.StringAttribute{Required: true, Description: "Instance UUID."},
			"sequence_id":        c("Sequence version UUID."),
			"tenant_id":          c("Tenant."),
			"namespace":          c("Namespace."),
			"state":              c("scheduled, running, waiting, paused, completed, failed or cancelled."),
			"priority":           c("Priority as serialized by the engine."),
			"timezone":           c("Instance timezone."),
			"next_fire_at":       c("Next wake-up time, if scheduled."),
			"idempotency_key":    c("Idempotency key used at creation, if any."),
			"parent_instance_id": c("Parent instance for sub-sequence children."),
			"metadata":           schema.StringAttribute{CustomType: jsontypes.NormalizedType{}, Computed: true, Description: "Instance metadata (JSON)."},
			"context":            schema.StringAttribute{CustomType: jsontypes.NormalizedType{}, Computed: true, Description: "Execution context (JSON)."},
			"created_at":         c("Creation timestamp."),
			"updated_at":         c("Last update timestamp."),
		},
	}
}

func rawToString(raw json.RawMessage) types.String {
	if len(raw) == 0 || string(raw) == "null" {
		return types.StringNull()
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return types.StringValue(s)
	}
	return types.StringValue(string(raw))
}

func rawToJSON(raw json.RawMessage) jsontypes.Normalized {
	if len(raw) == 0 || string(raw) == "null" {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(raw))
}

func (d *instanceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg instanceDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w instanceWire
	if err := d.client.Do(ctx, "GET", "/instances/"+client.PathEscape(cfg.ID.ValueString()), nil, nil, &w); err != nil {
		addAPIError(&resp.Diagnostics, "read instance", err)
		return
	}
	cfg.SequenceID = types.StringValue(w.SequenceID)
	cfg.TenantID = types.StringValue(w.TenantID)
	cfg.Namespace = types.StringValue(w.Namespace)
	cfg.State = types.StringValue(w.State)
	cfg.Priority = rawToString(w.Priority)
	cfg.Timezone = types.StringValue(w.Timezone)
	cfg.NextFireAt = stringOrNull(w.NextFireAt)
	cfg.IdempotencyKey = stringOrNull(w.IdempotencyKey)
	cfg.ParentInstanceID = stringOrNull(w.ParentInstanceID)
	cfg.Metadata = rawToJSON(w.Metadata)
	cfg.Context = rawToJSON(w.Context)
	cfg.CreatedAt = types.StringValue(w.CreatedAt)
	cfg.UpdatedAt = types.StringValue(w.UpdatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
