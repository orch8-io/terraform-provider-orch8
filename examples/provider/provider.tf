terraform {
  required_providers {
    orch8 = {
      source = "orch8-io/orch8"
    }
  }
}

# Every argument can also come from the environment:
# ORCH8_URL, ORCH8_API_KEY, ORCH8_TENANT_ID.
provider "orch8" {
  endpoint  = "http://127.0.0.1:8080"
  api_key   = var.orch8_api_key
  tenant_id = "acme"
}

variable "orch8_api_key" {
  type      = string
  sensitive = true
}
