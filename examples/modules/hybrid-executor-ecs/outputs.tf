output "cluster_arn" {
  description = "ECS cluster the executor runs in."
  value       = local.cluster_arn
}

output "service_name" {
  description = "ECS service name."
  value       = aws_ecs_service.this.name
}

output "join_token_secret_arn" {
  description = "Secrets Manager secret holding ORCH8_JOIN_TOKEN (rotate by updating var.join_token)."
  value       = aws_secretsmanager_secret.join.arn
}

output "security_group_ids" {
  description = "Security groups attached to the tasks."
  value       = local.sg_ids
}
