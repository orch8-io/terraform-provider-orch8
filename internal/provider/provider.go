// Package provider implements the Orch8 Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orch8-io/terraform-provider-orch8/internal/client"
)

var _ provider.Provider = (*orch8Provider)(nil)

type orch8Provider struct {
	version string
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
	TenantID types.String `tfsdk:"tenant_id"`
}

// New returns a provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &orch8Provider{version: version} }
}

func (p *orch8Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "orch8"
	resp.Version = p.version
}

func (p *orch8Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Orch8 durable-workflow engine resources (sequences, triggers, cron schedules, API keys, queue routing, rollback policies, credentials) through the engine's REST API (`/api/v1`).",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the Orch8 engine, e.g. `http://127.0.0.1:8080`. `/api/v1` is appended automatically. Defaults to `ORCH8_URL`, then `" + client.DefaultEndpoint + "`.",
			},
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "API key sent as `x-api-key`. Defaults to `ORCH8_API_KEY`. `orch8_api_key` requires the root (admin) key.",
			},
			"tenant_id": schema.StringAttribute{
				Optional:    true,
				Description: "Tenant sent as `x-tenant-id` and used as the default `tenant_id` of resources. Defaults to `ORCH8_TENANT_ID`.",
			},
		},
	}
}

func (p *orch8Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for attr, v := range map[string]types.String{"endpoint": cfg.Endpoint, "api_key": cfg.APIKey, "tenant_id": cfg.TenantID} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Unknown provider value",
				"The provider cannot be configured with a value that is unknown until apply. Set it statically or via environment variables.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := firstNonEmpty(cfg.Endpoint.ValueString(), os.Getenv("ORCH8_URL"))
	apiKey := firstNonEmpty(cfg.APIKey.ValueString(), os.Getenv("ORCH8_API_KEY"))
	tenant := firstNonEmpty(cfg.TenantID.ValueString(), os.Getenv("ORCH8_TENANT_ID"))

	c, err := client.New(endpoint, apiKey, tenant)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid endpoint", err.Error())
		return
	}
	c.UserAgent = "terraform-provider-orch8/" + p.version
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *orch8Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSequenceResource,
		NewTriggerResource,
		NewCronScheduleResource,
		NewAPIKeyResource,
		NewQueueRouteResource,
		NewQueueDispatchResource,
		NewRollbackPolicyResource,
		NewCredentialResource,
	}
}

func (p *orch8Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewSequenceDataSource,
		NewInstanceDataSource,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
