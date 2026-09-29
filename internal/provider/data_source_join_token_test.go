package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func validJoinToken() JoinToken {
	region := "eu-west-1"
	return JoinToken{
		V: 1, Endpoint: "https://control.orch8.io", APIKey: "k_secret", TenantID: "acme",
		RuntimeID: "018f5f2d-58ef-7a61-9b4f-21f77aa1f005", WorkerIDPrefix: "factory-1",
		Labels: map[string]string{"site": "berlin", "gpu": "a100"}, Region: &region,
	}
}

func TestJoinTokenRoundTrip(t *testing.T) {
	tok := validJoinToken()
	encoded, err := tok.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "o8x1.") || strings.ContainsAny(encoded, "+/=") {
		t.Fatalf("not an unpadded base64url o8x1 token: %s", encoded)
	}
	// Wire payload matches contract §5 field names exactly.
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, "o8x1."))
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "endpoint", "api_key", "tenant_id", "runtime_id", "worker_id_prefix", "labels", "region"} {
		if _, ok := payload[k]; !ok {
			t.Errorf("payload lacks %q", k)
		}
	}
	back, err := DecodeJoinToken(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if back.APIKey != tok.APIKey || back.Labels["gpu"] != "a100" || *back.Region != "eu-west-1" {
		t.Fatalf("round trip mismatch: %+v", back)
	}
	// Padded input (from other encoders) is accepted too.
	if _, err := DecodeJoinToken(encoded + "=="); err != nil {
		t.Fatalf("padded token rejected: %v", err)
	}
}

func TestJoinTokenNullRegionAndEmptyLabels(t *testing.T) {
	tok := validJoinToken()
	tok.Region, tok.Labels = nil, nil
	encoded, err := tok.Encode()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, "o8x1."))
	if !strings.Contains(string(raw), `"region":null`) || !strings.Contains(string(raw), `"labels":{}`) {
		t.Fatalf("unexpected payload %s", raw)
	}
}

func TestJoinTokenValidation(t *testing.T) {
	cases := map[string]func(*JoinToken){
		"endpoint must be an https":  func(j *JoinToken) { j.Endpoint = "http://control.orch8.io" },
		"endpoint must not contain":  func(j *JoinToken) { j.Endpoint = "https://a b" },
		"api_key must be":            func(j *JoinToken) { j.APIKey = " " },
		"tenant_id must not be":      func(j *JoinToken) { j.TenantID = "" },
		"runtime_id must be a UUID":  func(j *JoinToken) { j.RuntimeID = "nope" },
		"runtime_id must not be the": func(j *JoinToken) { j.RuntimeID = "00000000-0000-0000-0000-000000000000" },
		"worker_id_prefix must be":   func(j *JoinToken) { j.WorkerIDPrefix = "has space" },
		"label key":                  func(j *JoinToken) { j.Labels = map[string]string{"bad key": "v"} },
		"value must be at most":      func(j *JoinToken) { j.Labels = map[string]string{"k": strings.Repeat("x", 257)} },
		"region must be":             func(j *JoinToken) { r := "eu west"; j.Region = &r },
		"unsupported join token":     func(j *JoinToken) { j.V = 2 },
	}
	for want, mutate := range cases {
		tok := validJoinToken()
		mutate(&tok)
		if _, err := tok.Encode(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	if _, err := DecodeJoinToken("o8e1.abc"); err == nil {
		t.Error("wrong prefix accepted")
	}
	if _, err := DecodeJoinToken("o8x1.!!!"); err == nil {
		t.Error("bad base64 accepted")
	}
}

func TestDefaultRuntimeIDIsStable(t *testing.T) {
	a, b := DefaultRuntimeID("acme", "factory-1"), DefaultRuntimeID("acme", "factory-1")
	if a != b || a == DefaultRuntimeID("acme", "factory-2") {
		t.Fatalf("runtime id not stable/distinct: %s", a)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-`).MatchString(a) {
		t.Fatalf("not a UUIDv5: %s", a)
	}
}

func TestUnitExecutorJoinTokenDataSource(t *testing.T) {
	requireTerraformCLI(t)
	_, srv := newFakeEngine(t)
	decoded := func(check func(JoinToken) error) resource.CheckResourceAttrWithFunc {
		return func(v string) error {
			tok, err := DecodeJoinToken(v)
			if err != nil {
				return err
			}
			return check(tok)
		}
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerBlock(srv.URL) + `
resource "orch8_api_key" "executor" {
  name         = "hybrid-executor"
  capabilities = ["worker"]
}

data "orch8_executor_join_token" "k8s" {
  endpoint         = "https://control.orch8.io"
  api_key          = orch8_api_key.executor.secret
  worker_id_prefix = "k8s-berlin"
  labels           = { site = "berlin", gpu = "a100" }
  region           = "eu-central-1"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.orch8_executor_join_token.k8s", "tenant_id", "acme"),
					resource.TestCheckResourceAttr("data.orch8_executor_join_token.k8s", "id", "acme/k8s-berlin"),
					resource.TestCheckResourceAttr("data.orch8_executor_join_token.k8s", "runtime_id",
						DefaultRuntimeID("acme", "k8s-berlin")),
					resource.TestCheckResourceAttrWith("data.orch8_executor_join_token.k8s", "token",
						decoded(func(tok JoinToken) error {
							if tok.TenantID != "acme" || tok.Labels["site"] != "berlin" || tok.Region == nil ||
								*tok.Region != "eu-central-1" || tok.APIKey == "" {
								return fmt.Errorf("unexpected token %+v", tok)
							}
							return nil
						})),
					func(s *terraform.State) error {
						key := s.RootModule().Resources["orch8_api_key.executor"].Primary.Attributes["secret"]
						tokAttr := s.RootModule().Resources["data.orch8_executor_join_token.k8s"].Primary.Attributes["token"]
						tok, err := DecodeJoinToken(tokAttr)
						if err != nil {
							return err
						}
						if tok.APIKey != key {
							return fmt.Errorf("token api_key does not match the minted key")
						}
						return nil
					},
				),
			},
			{
				Config: providerBlock(srv.URL) + `
data "orch8_executor_join_token" "bad" {
  endpoint         = "http://insecure.example.com"
  api_key          = "k"
  worker_id_prefix = "x"
}
`,
				ExpectError: regexp.MustCompile(`endpoint must be an https:// URL`),
			},
		},
	})
}
