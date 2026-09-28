output "namespace" {
  description = "Namespace the executor runs in."
  value       = local.namespace
}

output "deployment_name" {
  description = "Executor Deployment name."
  value       = kubernetes_deployment_v1.this.metadata[0].name
}

output "secret_name" {
  description = "Secret holding ORCH8_JOIN_TOKEN."
  value       = kubernetes_secret_v1.join.metadata[0].name
}
