resource "orch8_cron_schedule" "weekday_digest" {
  sequence_id    = orch8_sequence.welcome.id
  cron_expr      = "0 0 9 * * MON-FRI"
  timezone       = "Europe/Kyiv"
  overlap_policy = "skip"
  metadata       = jsonencode({ source = "terraform" })
}
