package provider

// Acceptance tests against a real engine. They run only with TF_ACC=1:
//
//	docker compose up -d        # engine repo docker-compose.yml, needs ORCH8_API_KEY + ORCH8_ENCRYPTION_KEY
//	export ORCH8_URL=http://127.0.0.1:8080 ORCH8_API_KEY=... ORCH8_TENANT_ID=tf-acc
//	TF_ACC=1 go test ./internal/provider -run TestAcc -v
//
// Set ORCH8_ACC_API_KEYS=1 when ORCH8_API_KEY is the engine's root key to
// also exercise orch8_api_key.

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("ORCH8_URL") == "" {
		t.Fatal("ORCH8_URL must be set for acceptance tests")
	}
	if os.Getenv("ORCH8_TENANT_ID") == "" {
		t.Fatal("ORCH8_TENANT_ID must be set for acceptance tests")
	}
}

func accSuffix() string { return fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000) }

func testAccConfig(sfx, cron string, threshold float64) string {
	return fmt.Sprintf(`
provider "orch8" {}

resource "orch8_sequence" "v1" {
  name    = "tf-acc-%[1]s"
  version = 1
  definition = jsonencode({
    blocks = [{ type = "step", id = "log", handler = "log", params = { message = "hello from terraform" } }]
  })
}

resource "orch8_trigger" "hook" {
  slug          = "tf-acc-%[1]s"
  sequence_name = orch8_sequence.v1.name
  version       = orch8_sequence.v1.version
}

resource "orch8_cron_schedule" "c" {
  sequence_id = orch8_sequence.v1.id
  cron_expr   = %[2]q
  enabled     = false
}

resource "orch8_queue_route" "r" {
  handler_name   = "tf_acc_%[1]s"
  queue_override = "tf-acc-%[1]s"
}

resource "orch8_queue_dispatch" "d" {
  queue_name = "tf-acc-%[1]s"
  mode       = "poll"
}

resource "orch8_rollback_policy" "p" {
  sequence_name        = orch8_sequence.v1.name
  error_rate_threshold = %[3]v
  time_window_secs     = 300
}

resource "orch8_credential" "k" {
  id    = "tf-acc-%[1]s"
  name  = "tf acc"
  value = "not-a-real-secret"
}

data "orch8_sequence" "by_name" {
  name       = orch8_sequence.v1.name
  depends_on = [orch8_sequence.v1]
}
`, sfx, cron, threshold)
}

func TestAccCoreResources(t *testing.T) {
	sfx := accSuffix()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccConfig(sfx, "0 0 9 * * *", 0.2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orch8_sequence.v1", "id"),
					resource.TestCheckResourceAttrPair("data.orch8_sequence.by_name", "id", "orch8_sequence.v1", "id"),
					resource.TestCheckResourceAttr("orch8_trigger.hook", "enabled", "true"),
					resource.TestCheckResourceAttrSet("orch8_cron_schedule.c", "id"),
					resource.TestCheckResourceAttr("orch8_queue_dispatch.d", "mode", "poll"),
					resource.TestCheckResourceAttr("orch8_credential.k", "enabled", "true"),
				),
			},
			{
				Config: testAccConfig(sfx, "0 30 18 * * *", 0.4),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orch8_cron_schedule.c", "cron_expr", "0 30 18 * * *"),
					resource.TestCheckResourceAttr("orch8_rollback_policy.p", "error_rate_threshold", "0.4"),
				),
			},
			{ResourceName: "orch8_sequence.v1", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"definition", "warnings"}},
			{ResourceName: "orch8_trigger.hook", ImportState: true, ImportStateVerify: true},
			{ResourceName: "orch8_cron_schedule.c", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"next_fire_at"}},
			{ResourceName: "orch8_queue_route.r", ImportState: true, ImportStateVerify: true},
			{ResourceName: "orch8_queue_dispatch.d", ImportState: true, ImportStateVerify: true},
			{ResourceName: "orch8_rollback_policy.p", ImportState: true, ImportStateVerify: true},
			{ResourceName: "orch8_credential.k", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"value"}},
		},
	})
}

func TestAccAPIKey(t *testing.T) {
	if os.Getenv("ORCH8_ACC_API_KEYS") == "" {
		t.Skip("set ORCH8_ACC_API_KEYS=1 (with the root ORCH8_API_KEY) to test orch8_api_key")
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "orch8" {}
resource "orch8_api_key" "a" {
  name         = "tf-acc"
  capabilities = ["auditor"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orch8_api_key.a", "secret"),
					resource.TestCheckResourceAttr("orch8_api_key.a", "capabilities.#", "1"),
				),
			},
			{ResourceName: "orch8_api_key.a", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("orch8_api_key.a", "tenant_id", "id"), ImportStateVerifyIgnore: []string{"secret"}},
		},
	})
}
