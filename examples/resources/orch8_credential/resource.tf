# Referenced from sequences as "credentials://sendgrid".
resource "orch8_credential" "sendgrid" {
  id          = "sendgrid"
  name        = "SendGrid API key"
  kind        = "api_key"
  value       = var.sendgrid_api_key
  description = "Transactional email"
}

variable "sendgrid_api_key" {
  type      = string
  sensitive = true
}
