# A sequence version is immutable. Bump `version` to publish a new one.
resource "orch8_sequence" "welcome" {
  name    = "welcome-email"
  version = 1
  definition = jsonencode({
    input_schema = {
      type       = "object"
      required   = ["email"]
      properties = { email = { type = "string" } }
    }
    blocks = [
      {
        type    = "step"
        id      = "send"
        handler = "http_request"
        params = {
          method = "POST"
          url    = "https://mail.example.com/send"
          body   = { to = "{{ context.data.email }}", template = "welcome" }
        }
      }
    ]
  })
}

# Or load the same JSON you run locally with `orch8 dev`:
# definition = file("${path.module}/sequence.json")
