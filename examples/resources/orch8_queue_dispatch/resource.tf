# Push tasks on the `gpu` queue to a serverless worker instead of polling.
resource "orch8_queue_dispatch" "gpu" {
  queue_name = "gpu"
  mode       = "push"
  push_url   = "https://workers.example.com/orch8/push"
  secret     = var.push_secret
}

variable "push_secret" {
  type      = string
  sensitive = true
}
