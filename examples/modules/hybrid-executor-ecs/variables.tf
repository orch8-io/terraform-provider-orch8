variable "join_token" {
  description = "Executor join token (o8x1...). Issued by Orch8 Cloud, or composed with the orch8_executor_join_token data source. Stored in Secrets Manager."
  type        = string
  sensitive   = true

  validation {
    condition     = startswith(var.join_token, "o8x1.")
    error_message = "join_token must be an o8x1 executor join token."
  }
}

variable "name" {
  description = "Name prefix for the cluster, service, task definition, secret and log group."
  type        = string
  default     = "orch8-executor"
}

variable "labels" {
  description = "Tags for every AWS resource. Placement labels the executor advertises live in the join token."
  type        = map(string)
  default     = {}
}

variable "cluster_arn" {
  description = "Existing ECS cluster ARN. When null, a cluster named after var.name is created."
  type        = string
  default     = null
}

variable "subnet_ids" {
  description = "Subnets for the tasks. Executors only need outbound HTTPS to the control plane (private subnets with NAT are fine)."
  type        = list(string)
}

variable "security_group_ids" {
  description = "Security groups for the tasks. When empty, one with egress-only rules is created in var.vpc_id."
  type        = list(string)
  default     = []
}

variable "vpc_id" {
  description = "VPC for the created egress-only security group (required when security_group_ids is empty)."
  type        = string
  default     = null
}

variable "assign_public_ip" {
  description = "Assign public IPs (only for public subnets without NAT)."
  type        = bool
  default     = false
}

variable "desired_count" {
  description = "Executor tasks."
  type        = number
  default     = 2
}

variable "cpu" {
  description = "Fargate task CPU units."
  type        = number
  default     = 512
}

variable "memory" {
  description = "Fargate task memory (MiB)."
  type        = number
  default     = 1024
}

variable "cpu_architecture" {
  description = "X86_64 or ARM64."
  type        = string
  default     = "ARM64"
}

variable "image" {
  description = "Engine container image."
  type        = string
  default     = "ghcr.io/orch8-io/engine"
}

variable "image_tag" {
  description = "Engine image tag. Pin a version in production."
  type        = string
  default     = "latest"
}

variable "extra_env" {
  description = "Additional non-secret environment variables."
  type        = map(string)
  default     = {}
}

variable "log_retention_days" {
  description = "CloudWatch log retention."
  type        = number
  default     = 30
}

variable "secret_kms_key_id" {
  description = "KMS key for the join-token secret (default: the AWS managed key)."
  type        = string
  default     = null
}
