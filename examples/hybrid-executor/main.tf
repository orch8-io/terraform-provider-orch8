# End-to-end: mint a worker key on the engine, compose a join token with
# placement labels, and run executors on Kubernetes. With Orch8 Cloud, drop
# the key + data source and pass the join token the Cloud console issues.
terraform {
  required_providers {
    orch8      = { source = "orch8-io/orch8" }
    kubernetes = { source = "hashicorp/kubernetes", version = ">= 2.23" }
  }
}

provider "orch8" {} # ORCH8_URL, ORCH8_API_KEY (root key, to mint keys), ORCH8_TENANT_ID

provider "kubernetes" {
  config_path = "~/.kube/config"
}

variable "control_endpoint" {
  description = "Managed control-plane endpoint executors dial out to (https://)."
  type        = string
}

variable "labels" {
  description = "Placement labels, matched by step/sequence placement.labels."
  type        = map(string)
  default     = { site = "berlin", gpu = "a100" }
}

resource "orch8_api_key" "executor" {
  name         = "hybrid-executor-berlin"
  capabilities = ["worker"]
}

data "orch8_executor_join_token" "berlin" {
  endpoint         = var.control_endpoint
  api_key          = orch8_api_key.executor.secret
  worker_id_prefix = "berlin-k8s"
  labels           = var.labels
  region           = "eu-central-1"
}

module "executor" {
  source = "../modules/hybrid-executor-k8s"

  join_token = data.orch8_executor_join_token.berlin.token
  labels     = { "orch8.io/site" = var.labels["site"] }
  replicas   = 3
  image_tag  = "latest" # pin an engine release in production
  node_selector = {
    "nvidia.com/gpu.present" = "true"
  }
}

# ECS variant (same token):
# module "executor_ecs" {
#   source     = "../modules/hybrid-executor-ecs"
#   join_token = data.orch8_executor_join_token.berlin.token
#   labels     = { site = "berlin" }
#   subnet_ids = ["subnet-0123456789abcdef0"]
#   vpc_id     = "vpc-0123456789abcdef0"
# }
