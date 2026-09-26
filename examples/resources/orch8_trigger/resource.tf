resource "orch8_trigger" "signup" {
  slug          = "user-signup"
  sequence_name = orch8_sequence.welcome.name
  version       = orch8_sequence.welcome.version # pinned: bumping retargets in place
  secret        = var.signup_webhook_secret
}

variable "signup_webhook_secret" {
  type      = string
  sensitive = true
}
