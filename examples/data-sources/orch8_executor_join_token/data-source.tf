# Mint a dedicated worker key (needs the engine's root API key) and wrap it in
# an executor join token. Orch8 Cloud users can skip this and paste the join
# token the Cloud console issues.
resource "orch8_api_key" "executor" {
  name         = "hybrid-executor-berlin"
  capabilities = ["worker"]
}

data "orch8_executor_join_token" "berlin" {
  endpoint         = "https://control.orch8.example.com"
  api_key          = orch8_api_key.executor.secret
  worker_id_prefix = "berlin-k8s"
  labels           = { site = "berlin", gpu = "a100" }
  region           = "eu-central-1"
}

output "join_token" {
  value     = data.orch8_executor_join_token.berlin.token
  sensitive = true
}
