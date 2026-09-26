package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// These tests drive the real Terraform CLI against the provider and an
// in-memory fake engine (fake_engine_test.go). They run without TF_ACC.

func providerBlock(url string) string {
	return fmt.Sprintf(`
provider "orch8" {
  endpoint  = %q
  api_key   = "test-key"
  tenant_id = "acme"
}
`, url)
}

func configV1(url string, cron, dispatchMode, credName string, threshold float64, triggerVersion int) string {
	return providerBlock(url) + fmt.Sprintf(`
resource "orch8_sequence" "v1" {
  name    = "welcome"
  version = 1
  definition = jsonencode({
    blocks = [{ type = "step", id = "greet", handler = "log", params = { message = "hi" } }]
  })
}

resource "orch8_sequence" "v2" {
  name    = "welcome"
  version = 2
  definition = jsonencode({
    blocks = [{ type = "step", id = "greet", handler = "log", params = { message = "hello" } }]
  })
}

resource "orch8_trigger" "hook" {
  slug          = "welcome-hook"
  sequence_name = "welcome"
  version       = %d
  secret        = "s3cret"
  depends_on    = [orch8_sequence.v1, orch8_sequence.v2]
}

resource "orch8_cron_schedule" "daily" {
  sequence_id = orch8_sequence.v1.id
  cron_expr   = %q
  timezone    = "Europe/Kyiv"
  metadata    = jsonencode({ source = "terraform" })
}

resource "orch8_api_key" "worker" {
  name         = "worker"
  capabilities = ["worker"]
}

resource "orch8_queue_route" "gpu" {
  handler_name   = "render"
  queue_override = "gpu"
  priority       = 10
}

resource "orch8_queue_dispatch" "gpu" {
  queue_name = "gpu"
  mode       = %q
  push_url   = %s
}

resource "orch8_rollback_policy" "welcome" {
  sequence_name        = "welcome"
  error_rate_threshold = %v
  time_window_secs     = 300
}

resource "orch8_credential" "slack" {
  id    = "slack-token"
  name  = %q
  value = "xoxb-123"
}

data "orch8_sequence" "latest" {
  name       = "welcome"
  depends_on = [orch8_sequence.v1, orch8_sequence.v2]
}
`, triggerVersion, cron, dispatchMode, pushURL(dispatchMode), threshold, credName)
}

func pushURL(mode string) string {
	if mode == "push" {
		return `"https://workers.example.com/push"`
	}
	return "null"
}

func TestUnitResourcesLifecycle(t *testing.T) {
	requireTerraformCLI(t)
	fake, srv := newFakeEngine(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if n := len(fake.seqs) + len(fake.triggers) + len(fake.crons) + len(fake.rules) +
				len(fake.dispatch) + len(fake.policies) + len(fake.creds); n != 0 {
				return fmt.Errorf("%d objects left after destroy", n)
			}
			for id, k := range fake.keys {
				if k["revoked"] != true {
					return fmt.Errorf("api key %s not revoked", id)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: configV1(srv.URL, "0 0 9 * * *", "poll", "Slack", 0.2, 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orch8_sequence.v1", "id"),
					resource.TestCheckResourceAttr("orch8_sequence.v1", "tenant_id", "acme"),
					resource.TestCheckResourceAttr("orch8_sequence.v1", "namespace", "default"),
					resource.TestCheckResourceAttr("orch8_sequence.v1", "status", "production"),
					resource.TestCheckResourceAttr("orch8_sequence.v1", "warnings.#", "1"),
					resource.TestCheckResourceAttr("orch8_trigger.hook", "version", "1"),
					resource.TestCheckResourceAttr("orch8_trigger.hook", "trigger_type", "webhook"),
					resource.TestCheckResourceAttr("orch8_trigger.hook", "enabled", "true"),
					resource.TestCheckResourceAttr("orch8_trigger.hook", "secret", "s3cret"),
					resource.TestCheckResourceAttrPair("orch8_cron_schedule.daily", "sequence_id", "orch8_sequence.v1", "id"),
					resource.TestCheckResourceAttr("orch8_cron_schedule.daily", "overlap_policy", "allow"),
					resource.TestCheckResourceAttr("orch8_cron_schedule.daily", "enabled", "true"),
					resource.TestCheckResourceAttrSet("orch8_api_key.worker", "secret"),
					resource.TestCheckResourceAttr("orch8_api_key.worker", "capabilities.#", "1"),
					resource.TestCheckResourceAttr("orch8_queue_route.gpu", "enabled", "true"),
					resource.TestCheckResourceAttr("orch8_queue_dispatch.gpu", "id", "acme/gpu"),
					resource.TestCheckResourceAttr("orch8_rollback_policy.welcome", "cooldown_secs", "3600"),
					resource.TestCheckResourceAttr("orch8_rollback_policy.welcome", "id", "acme/welcome"),
					resource.TestCheckResourceAttr("orch8_credential.slack", "kind", "api_key"),
					resource.TestCheckResourceAttr("orch8_credential.slack", "enabled", "true"),
					resource.TestCheckResourceAttr("data.orch8_sequence.latest", "version", "2"),
				),
			},
			// In-place updates: cron PUT, dispatch upsert, rollback upsert,
			// credential PATCH, trigger retarget to v2.
			{
				Config: configV1(srv.URL, "0 30 18 * * *", "push", "Slack bot", 0.5, 2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orch8_cron_schedule.daily", "cron_expr", "0 30 18 * * *"),
					resource.TestCheckResourceAttr("orch8_queue_dispatch.gpu", "mode", "push"),
					resource.TestCheckResourceAttr("orch8_rollback_policy.welcome", "error_rate_threshold", "0.5"),
					resource.TestCheckResourceAttr("orch8_credential.slack", "name", "Slack bot"),
					resource.TestCheckResourceAttr("orch8_trigger.hook", "version", "2"),
					func(_ *terraform.State) error {
						fake.mu.Lock()
						defer fake.mu.Unlock()
						for _, want := range []string{"PUT /api/v1/cron/", "PATCH /api/v1/triggers/welcome-hook/target", "PATCH /api/v1/credentials/slack-token"} {
							if !calledPrefix(fake.calls, want) {
								return fmt.Errorf("expected a call %q, calls: %v", want, fake.calls)
							}
						}
						return nil
					},
				),
			},
			{ResourceName: "orch8_sequence.v1", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"definition", "warnings"}},
			{ResourceName: "orch8_trigger.hook", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"secret"}},
			{ResourceName: "orch8_cron_schedule.daily", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"next_fire_at"}},
			{ResourceName: "orch8_api_key.worker", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("orch8_api_key.worker", "tenant_id", "id"), ImportStateVerifyIgnore: []string{"secret"}},
			{ResourceName: "orch8_queue_route.gpu", ImportState: true, ImportStateVerify: true},
			{ResourceName: "orch8_queue_dispatch.gpu", ImportState: true, ImportStateVerify: true},
			{ResourceName: "orch8_rollback_policy.welcome", ImportState: true, ImportStateVerify: true},
			{ResourceName: "orch8_credential.slack", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"value"}},
		},
	})
}

func calledPrefix(calls []string, prefix string) bool {
	for _, c := range calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func importID(name string, attrs ...string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("%s not in state", name)
		}
		id := ""
		for i, a := range attrs {
			if i > 0 {
				id += "/"
			}
			id += rs.Primary.Attributes[a]
		}
		return id, nil
	}
}

func TestUnitResourceRemovedOutOfBand(t *testing.T) {
	requireTerraformCLI(t)
	fake, srv := newFakeEngine(t)
	cfg := providerBlock(srv.URL) + `
resource "orch8_credential" "c" {
  id    = "gone"
  name  = "Gone"
  value = "v"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					fake.mu.Lock()
					delete(fake.creds, "gone")
					fake.mu.Unlock()
				},
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestUnitInstanceDataSource(t *testing.T) {
	requireTerraformCLI(t)
	fake, srv := newFakeEngine(t)
	fake.inst["00000000-0000-7000-8000-000000000099"] = map[string]any{
		"id": "00000000-0000-7000-8000-000000000099", "sequence_id": "s1", "tenant_id": "acme", "namespace": "default",
		"state": "completed", "priority": "normal", "timezone": "UTC", "next_fire_at": nil,
		"metadata": map[string]any{"k": "v"}, "context": map[string]any{"data": map[string]any{"x": 1}},
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:01Z",
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(srv.URL) + `
data "orch8_instance" "i" { id = "00000000-0000-7000-8000-000000000099" }
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.orch8_instance.i", "state", "completed"),
				resource.TestCheckResourceAttr("data.orch8_instance.i", "priority", "normal"),
				resource.TestCheckResourceAttr("data.orch8_instance.i", "metadata", `{"k":"v"}`),
			),
		}},
	})
}
