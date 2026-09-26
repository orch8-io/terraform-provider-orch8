package provider

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used by unit (fake engine) and
// acceptance (TF_ACC) tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"orch8": providerserver.NewProtocol6WithError(New("test")()),
}

// requireTerraformCLI skips tests that drive the real Terraform CLI when no
// working binary is available. terraform-plugin-testing honours
// TF_ACC_TERRAFORM_PATH; otherwise it looks up `terraform` on PATH.
func requireTerraformCLI(t *testing.T) {
	t.Helper()
	bin := os.Getenv("TF_ACC_TERRAFORM_PATH")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("terraform"); err != nil {
			t.Skip("terraform CLI not found; set TF_ACC_TERRAFORM_PATH to run CLI-driven unit tests")
		}
	}
	if err := exec.Command(bin, "version").Run(); err != nil {
		t.Skipf("terraform CLI at %s is not usable (%v); set TF_ACC_TERRAFORM_PATH", bin, err)
	}
}

func TestProviderSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	var presp fwprovider.SchemaResponse
	p.Schema(ctx, fwprovider.SchemaRequest{}, &presp)
	if presp.Diagnostics.HasError() {
		t.Fatalf("provider schema: %v", presp.Diagnostics)
	}
	if !presp.Schema.Attributes["api_key"].IsSensitive() {
		t.Error("api_key must be sensitive")
	}

	names := map[string]bool{}
	for _, f := range p.Resources(ctx) {
		r := f()
		var m resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "orch8"}, &m)
		names[m.TypeName] = true
		var s resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &s)
		if s.Diagnostics.HasError() {
			t.Fatalf("%s schema: %v", m.TypeName, s.Diagnostics)
		}
		if d := s.Schema.ValidateImplementation(ctx); d.HasError() {
			t.Fatalf("%s schema invalid: %v", m.TypeName, d)
		}
		if _, ok := r.(resource.ResourceWithImportState); !ok {
			t.Errorf("%s does not support import", m.TypeName)
		}
		if s.Schema.Description == "" {
			t.Errorf("%s has no description", m.TypeName)
		}
	}
	for _, want := range []string{"orch8_sequence", "orch8_trigger", "orch8_cron_schedule", "orch8_api_key",
		"orch8_queue_route", "orch8_queue_dispatch", "orch8_rollback_policy", "orch8_credential"} {
		if !names[want] {
			t.Errorf("resource %s not registered", want)
		}
	}
	for _, f := range p.DataSources(ctx) {
		d := f()
		var m datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "orch8"}, &m)
		var s datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &s)
		if dd := s.Schema.ValidateImplementation(ctx); dd.HasError() {
			t.Fatalf("%s schema invalid: %v", m.TypeName, dd)
		}
	}
	// Sensitive write-only attributes.
	sensitive := map[string][]string{
		"orch8_api_key":        {"secret"},
		"orch8_credential":     {"value", "refresh_token"},
		"orch8_trigger":        {"secret"},
		"orch8_queue_dispatch": {"secret"},
	}
	for _, f := range p.Resources(ctx) {
		r := f()
		var m resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "orch8"}, &m)
		var s resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &s)
		for _, a := range sensitive[m.TypeName] {
			if !s.Schema.Attributes[a].IsSensitive() {
				t.Errorf("%s.%s must be sensitive", m.TypeName, a)
			}
		}
	}
}
