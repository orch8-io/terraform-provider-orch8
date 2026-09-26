# Latest version by name.
data "orch8_sequence" "welcome" {
  name      = "welcome-email"
  namespace = "default"
}

# A specific version.
data "orch8_sequence" "welcome_v1" {
  name    = "welcome-email"
  version = 1
}

output "welcome_latest_id" {
  value = data.orch8_sequence.welcome.id
}
