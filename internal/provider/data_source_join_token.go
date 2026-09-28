package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// JoinTokenPrefix is the wire prefix of an executor join token (format v1).
const JoinTokenPrefix = "o8x1."

// joinRuntimeNamespace seeds the deterministic runtime id derived when
// `runtime_id` is omitted (UUIDv5 over "<tenant>/<worker_id_prefix>").
var joinRuntimeNamespace = uuid.MustParse("6f0e5a52-3c1d-5b8e-9a37-0b8c2d1e4f60")

var joinIdentRe = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// JoinToken is the decoded payload of `o8x1.<b64url(json)>`: everything an
// executor node needs to open its outbound managed-control session. It is a
// secret (it carries an API key) and is deliberately unsigned.
type JoinToken struct {
	V              int               `json:"v"`
	Endpoint       string            `json:"endpoint"`
	APIKey         string            `json:"api_key"`
	TenantID       string            `json:"tenant_id"`
	RuntimeID      string            `json:"runtime_id"`
	WorkerIDPrefix string            `json:"worker_id_prefix"`
	Labels         map[string]string `json:"labels"`
	Region         *string           `json:"region"`
}

func joinIdent(v string, maxLen int) bool {
	return v != "" && len(v) <= maxLen && joinIdentRe.MatchString(v)
}

// Validate mirrors the engine's parser (orch8-types join_token.rs) so a token
// Terraform produces is never rejected at container start.
func (t JoinToken) Validate() error {
	if t.V != 1 {
		return fmt.Errorf("unsupported join token version %d", t.V)
	}
	ep := strings.TrimSpace(t.Endpoint)
	if !strings.HasPrefix(ep, "https://") || len(ep) <= len("https://") {
		return fmt.Errorf("endpoint must be an https:// URL")
	}
	if strings.IndexFunc(ep, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("endpoint must not contain whitespace")
	}
	if strings.TrimSpace(t.APIKey) == "" || len(t.APIKey) > 1024 {
		return fmt.Errorf("api_key must be 1-1024 chars")
	}
	if strings.TrimSpace(t.TenantID) == "" {
		return fmt.Errorf("tenant_id must not be empty")
	}
	id, err := uuid.Parse(t.RuntimeID)
	if err != nil {
		return fmt.Errorf("runtime_id must be a UUID")
	}
	if id == uuid.Nil {
		return fmt.Errorf("runtime_id must not be the nil UUID")
	}
	if !joinIdent(t.WorkerIDPrefix, 64) {
		return fmt.Errorf("worker_id_prefix must be 1-64 chars of [A-Za-z0-9._:-]")
	}
	if len(t.Labels) > 64 {
		return fmt.Errorf("at most 64 labels")
	}
	for k, v := range t.Labels {
		if !joinIdent(k, 63) {
			return fmt.Errorf("label key %q must be 1-63 chars of [A-Za-z0-9._:-]", k)
		}
		if len(v) > 256 || strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return fmt.Errorf("label %q value must be at most 256 chars without control characters", k)
		}
	}
	if t.Region != nil && !joinIdent(*t.Region, 64) {
		return fmt.Errorf("region must be 1-64 chars of [A-Za-z0-9._:-]")
	}
	return nil
}

// Encode returns the wire form `o8x1.<base64url, unpadded(json)>`.
func (t JoinToken) Encode() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return JoinTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeJoinToken parses and validates a wire token.
func DecodeJoinToken(raw string) (JoinToken, error) {
	var t JoinToken
	body, ok := strings.CutPrefix(strings.TrimSpace(raw), JoinTokenPrefix)
	if !ok {
		return t, fmt.Errorf("join token must start with %q", JoinTokenPrefix)
	}
	bytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(body, "="))
	if err != nil {
		return t, fmt.Errorf("join token payload is not valid base64url")
	}
	if err := json.Unmarshal(bytes, &t); err != nil {
		return t, fmt.Errorf("join token payload is not valid JSON: %w", err)
	}
	return t, t.Validate()
}

// DefaultRuntimeID is the stable runtime id used when none is configured.
func DefaultRuntimeID(tenant, workerIDPrefix string) string {
	return uuid.NewSHA1(joinRuntimeNamespace, []byte(tenant+"/"+workerIDPrefix)).String()
}

// ---------------------------------------------------------------- data source

// NewExecutorJoinTokenDataSource composes an executor join token.
func NewExecutorJoinTokenDataSource() datasource.DataSource { return &joinTokenDataSource{} }

type joinTokenDataSource struct{ dsBase }

type joinTokenModel struct {
	ID             types.String `tfsdk:"id"`
	Endpoint       types.String `tfsdk:"endpoint"`
	APIKey         types.String `tfsdk:"api_key"`
	TenantID       types.String `tfsdk:"tenant_id"`
	RuntimeID      types.String `tfsdk:"runtime_id"`
	WorkerIDPrefix types.String `tfsdk:"worker_id_prefix"`
	Labels         types.Map    `tfsdk:"labels"`
	Region         types.String `tfsdk:"region"`
	Token          types.String `tfsdk:"token"`
}

func (d *joinTokenDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_executor_join_token"
}

func (d *joinTokenDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Compose an executor join token (`o8x1.<base64url(json)>`) from an API key and executor identity, " +
			"for `ORCH8_JOIN_TOKEN` / `orch8 executor join`. Computed locally (no API call) and validated with the " +
			"engine's rules. The token contains the API key: it is sensitive and lands in state like the key itself. " +
			"Orch8 Cloud issues ready-made join tokens; use this data source when you mint the key yourself " +
			"(for example with `orch8_api_key`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "`<tenant_id>/<worker_id_prefix>`."},
			"endpoint": schema.StringAttribute{Required: true,
				Description: "Managed control-plane endpoint the executor connects out to (`https://` only)."},
			"api_key": schema.StringAttribute{Required: true, Sensitive: true,
				Description: "Dedicated API key the executor authenticates with (e.g. `orch8_api_key.x.secret`)."},
			"tenant_id": schema.StringAttribute{Optional: true, Computed: true,
				Description: "Tenant; defaults to the provider's tenant_id."},
			"runtime_id": schema.StringAttribute{Optional: true, Computed: true,
				Description: "Runtime UUID. Defaults to a stable UUIDv5 of `<tenant_id>/<worker_id_prefix>`."},
			"worker_id_prefix": schema.StringAttribute{Required: true,
				Description: "Worker id prefix; each replica appends its hostname. 1-64 chars of `[A-Za-z0-9._:-]`."},
			"labels": schema.MapAttribute{Optional: true, ElementType: types.StringType,
				Description: "Placement labels advertised by the executor (matched by `placement.labels`)."},
			"region": schema.StringAttribute{Optional: true,
				Description: "Region advertised by the executor (matched by `placement.region`)."},
			"token": schema.StringAttribute{Computed: true, Sensitive: true,
				Description: "The join token. Pass it to the container as `ORCH8_JOIN_TOKEN`."},
		},
	}
}

func (d *joinTokenDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg joinTokenModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tenant := d.tenantFor(cfg.TenantID)
	if !requireTenant(tenant, &resp.Diagnostics) {
		return
	}
	labels := map[string]string{}
	if !cfg.Labels.IsNull() && !cfg.Labels.IsUnknown() {
		resp.Diagnostics.Append(cfg.Labels.ElementsAs(ctx, &labels, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	runtimeID := cfg.RuntimeID.ValueString()
	if cfg.RuntimeID.IsNull() || runtimeID == "" {
		runtimeID = DefaultRuntimeID(tenant, cfg.WorkerIDPrefix.ValueString())
	}
	tok := JoinToken{
		V:              1,
		Endpoint:       strings.TrimSpace(cfg.Endpoint.ValueString()),
		APIKey:         cfg.APIKey.ValueString(),
		TenantID:       tenant,
		RuntimeID:      runtimeID,
		WorkerIDPrefix: cfg.WorkerIDPrefix.ValueString(),
		Labels:         labels,
	}
	if !cfg.Region.IsNull() && cfg.Region.ValueString() != "" {
		r := cfg.Region.ValueString()
		tok.Region = &r
	}
	encoded, err := tok.Encode()
	if err != nil {
		addJoinError(&resp.Diagnostics, err)
		return
	}
	cfg.ID = types.StringValue(tenant + "/" + tok.WorkerIDPrefix)
	cfg.TenantID = types.StringValue(tenant)
	cfg.RuntimeID = types.StringValue(runtimeID)
	cfg.Token = types.StringValue(encoded)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

func addJoinError(diags *diag.Diagnostics, err error) {
	diags.AddAttributeError(path.Root("token"), "Invalid executor join token", err.Error())
}
