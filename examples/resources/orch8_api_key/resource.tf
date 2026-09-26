# Requires the provider to authenticate with the engine's root API key.
resource "orch8_api_key" "workers" {
  name         = "gpu-workers"
  capabilities = ["worker"]
}

output "worker_key" {
  value     = orch8_api_key.workers.secret
  sensitive = true
}
