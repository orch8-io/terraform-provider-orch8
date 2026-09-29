variable "join_token" {
  description = "Executor join token (o8x1...). Issued by Orch8 Cloud, or composed with the orch8_executor_join_token data source."
  type        = string
  sensitive   = true

  validation {
    condition     = startswith(var.join_token, "o8x1.")
    error_message = "join_token must be an o8x1 executor join token."
  }
}

variable "name" {
  description = "Name of the Deployment, Secret and ServiceAccount."
  type        = string
  default     = "orch8-executor"
}

variable "namespace" {
  description = "Kubernetes namespace (created when create_namespace is true)."
  type        = string
  default     = "orch8"
}

variable "create_namespace" {
  description = "Create the namespace."
  type        = bool
  default     = true
}

variable "labels" {
  description = "Extra Kubernetes labels for every object. Placement labels the executor advertises live in the join token."
  type        = map(string)
  default     = {}
}

variable "replicas" {
  description = "Executor replicas. Each pod joins with worker id <worker_id_prefix>-<pod name>."
  type        = number
  default     = 2
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

variable "resources" {
  description = "Container resource requests and limits."
  type = object({
    requests = map(string)
    limits   = map(string)
  })
  default = {
    requests = { cpu = "250m", memory = "256Mi" }
    limits   = { cpu = "2", memory = "1Gi" }
  }
}

variable "node_selector" {
  description = "Node selector for executor pods (e.g. GPU nodes)."
  type        = map(string)
  default     = {}
}

variable "extra_env" {
  description = "Additional non-secret environment variables (e.g. ORCH8_LOG_LEVEL)."
  type        = map(string)
  default     = {}
}
