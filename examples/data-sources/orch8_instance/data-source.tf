data "orch8_instance" "run" {
  id = var.instance_id
}

variable "instance_id" {
  type = string
}

output "run_state" {
  value = data.orch8_instance.run.state
}
