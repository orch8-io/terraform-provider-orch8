terraform {
  required_version = ">= 1.5"
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.23"
    }
  }
}

locals {
  labels = merge(var.labels, {
    "app.kubernetes.io/name"       = "orch8-executor"
    "app.kubernetes.io/instance"   = var.name
    "app.kubernetes.io/component"  = "executor"
    "app.kubernetes.io/managed-by" = "terraform"
  })
  selector = {
    "app.kubernetes.io/name"     = "orch8-executor"
    "app.kubernetes.io/instance" = var.name
  }
  namespace = var.create_namespace ? kubernetes_namespace_v1.this[0].metadata[0].name : var.namespace
}

resource "kubernetes_namespace_v1" "this" {
  count = var.create_namespace ? 1 : 0
  metadata {
    name   = var.namespace
    labels = local.labels
  }
}

# The join token carries an API key: keep it in a Secret, never in the pod spec.
resource "kubernetes_secret_v1" "join" {
  metadata {
    name      = "${var.name}-join"
    namespace = local.namespace
    labels    = local.labels
  }
  data = {
    ORCH8_JOIN_TOKEN = var.join_token
  }
}

resource "kubernetes_service_account_v1" "this" {
  metadata {
    name      = var.name
    namespace = local.namespace
    labels    = local.labels
  }
  automount_service_account_token = false
}

resource "kubernetes_deployment_v1" "this" {
  metadata {
    name      = var.name
    namespace = local.namespace
    labels    = local.labels
  }

  spec {
    replicas = var.replicas
    selector {
      match_labels = local.selector
    }

    template {
      metadata {
        labels = local.labels
        annotations = {
          # Roll the pods when the token changes.
          "orch8.io/join-token-sha256" = sha256(var.join_token)
        }
      }

      spec {
        service_account_name             = kubernetes_service_account_v1.this.metadata[0].name
        automount_service_account_token  = false
        node_selector                    = var.node_selector
        termination_grace_period_seconds = 60

        # uid/gid 999 is the image's `orch8` user (same as the Helm chart).
        security_context {
          run_as_non_root = true
          run_as_user     = 999
          run_as_group    = 999
          fs_group        = 999
        }

        container {
          name  = "executor"
          image = "${var.image}:${var.image_tag}"

          # The engine reads ORCH8_JOIN_TOKEN at start: role=executor plus the
          # managed-control endpoint, key, tenant, runtime id, labels and region;
          # HOSTNAME (the pod name) suffixes the worker id.
          env_from {
            secret_ref {
              name = kubernetes_secret_v1.join.metadata[0].name
            }
          }

          dynamic "env" {
            for_each = var.extra_env
            content {
              name  = env.key
              value = env.value
            }
          }

          port {
            name           = "http"
            container_port = 8080
          }

          resources {
            requests = var.resources.requests
            limits   = var.resources.limits
          }

          liveness_probe {
            http_get {
              path = "/health/live"
              port = "http"
            }
            initial_delay_seconds = 10
            period_seconds        = 15
          }

          readiness_probe {
            http_get {
              path = "/health/ready"
              port = "http"
            }
            period_seconds = 10
          }

          security_context {
            allow_privilege_escalation = false
            read_only_root_filesystem  = true
            capabilities {
              drop = ["ALL"]
            }
          }

          volume_mount {
            name       = "data"
            mount_path = "/data"
          }
          volume_mount {
            name       = "tmp"
            mount_path = "/tmp"
          }
        }

        # Local SQLite scratch state of the executor; work is owned by the control plane.
        volume {
          name = "data"
          empty_dir {}
        }
        volume {
          name = "tmp"
          empty_dir {}
        }
      }
    }
  }
}
