resource "orch8_rollback_policy" "welcome" {
  sequence_name            = orch8_sequence.welcome.name
  error_rate_threshold     = 0.05
  time_window_secs         = 600
  cooldown_secs            = 3600
  confirmation_window_secs = 60
  webhook_url              = "https://alerts.example.com/orch8-rollback"
}
