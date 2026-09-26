# Send every `render_video` task to the `gpu` queue.
resource "orch8_queue_route" "render_on_gpu" {
  handler_name   = "render_video"
  queue_override = "gpu"
  priority       = 10
}
