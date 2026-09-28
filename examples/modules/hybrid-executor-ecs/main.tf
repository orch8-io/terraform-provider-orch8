terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
  }
}

data "aws_region" "current" {}

locals {
  tags        = merge(var.labels, { "app" = "orch8-executor", "managed-by" = "terraform" })
  cluster_arn = coalesce(var.cluster_arn, try(aws_ecs_cluster.this[0].arn, null))
  sg_ids      = length(var.security_group_ids) > 0 ? var.security_group_ids : [aws_security_group.egress[0].id]
  environment = [for k, v in var.extra_env : { name = k, value = v }]
}

resource "aws_ecs_cluster" "this" {
  count = var.cluster_arn == null ? 1 : 0
  name  = var.name
  tags  = local.tags

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

# The join token carries an API key: it goes to Secrets Manager and reaches the
# container through the task execution role, never as a plain env value.
resource "aws_secretsmanager_secret" "join" {
  name_prefix = "${var.name}-join-"
  description = "Orch8 executor join token (ORCH8_JOIN_TOKEN)"
  kms_key_id  = var.secret_kms_key_id
  tags        = local.tags
}

resource "aws_secretsmanager_secret_version" "join" {
  secret_id     = aws_secretsmanager_secret.join.id
  secret_string = var.join_token
}

resource "aws_cloudwatch_log_group" "this" {
  name              = "/ecs/${var.name}"
  retention_in_days = var.log_retention_days
  tags              = local.tags
}

data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "execution" {
  name_prefix        = substr("${var.name}-exec-", 0, 38)
  assume_role_policy = data.aws_iam_policy_document.assume.json
  tags               = local.tags
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

data "aws_iam_policy_document" "read_secret" {
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.join.arn]
  }
}

resource "aws_iam_role_policy" "read_secret" {
  name   = "read-join-token"
  role   = aws_iam_role.execution.id
  policy = data.aws_iam_policy_document.read_secret.json
}

# The executor itself needs no AWS permissions.
resource "aws_iam_role" "task" {
  name_prefix        = substr("${var.name}-task-", 0, 38)
  assume_role_policy = data.aws_iam_policy_document.assume.json
  tags               = local.tags
}

resource "aws_security_group" "egress" {
  count       = length(var.security_group_ids) == 0 ? 1 : 0
  name_prefix = "${var.name}-"
  description = "Orch8 executor: outbound only (it dials the control plane)"
  vpc_id      = var.vpc_id
  tags        = local.tags

  egress {
    description = "Outbound HTTPS/gRPC to the control plane and step targets"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  lifecycle {
    precondition {
      condition     = var.vpc_id != null
      error_message = "Set vpc_id, or pass security_group_ids."
    }
  }
}

resource "aws_ecs_task_definition" "this" {
  family                   = var.name
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.cpu
  memory                   = var.memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn
  tags                     = local.tags

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = var.cpu_architecture
  }

  container_definitions = jsonencode([{
    name      = "executor"
    image     = "${var.image}:${var.image_tag}"
    essential = true
    # The engine reads ORCH8_JOIN_TOKEN at start: role=executor plus the
    # managed-control endpoint, key, tenant, runtime id, labels and region;
    # HOSTNAME suffixes the worker id so every task is a distinct worker.
    secrets = [{
      name      = "ORCH8_JOIN_TOKEN"
      valueFrom = aws_secretsmanager_secret.join.arn
    }]
    environment     = local.environment
    portMappings    = [{ containerPort = 8080, protocol = "tcp" }]
    stopTimeout     = 60
    user            = "999:999"
    linuxParameters = { initProcessEnabled = true }
    healthCheck = {
      command     = ["CMD", "/usr/local/bin/orch8", "--url", "http://127.0.0.1:8080", "health"]
      interval    = 15
      timeout     = 5
      retries     = 3
      startPeriod = 15
    }
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.this.name
        awslogs-region        = data.aws_region.current.region
        awslogs-stream-prefix = "executor"
      }
    }
  }])

  depends_on = [aws_secretsmanager_secret_version.join]
}

resource "aws_ecs_service" "this" {
  name                   = var.name
  cluster                = local.cluster_arn
  task_definition        = aws_ecs_task_definition.this.arn
  desired_count          = var.desired_count
  launch_type            = "FARGATE"
  enable_execute_command = false
  propagate_tags         = "SERVICE"
  tags                   = local.tags

  # A new join token only reaches running tasks through a new deployment.
  force_new_deployment = true
  triggers = {
    join_token = sha256(var.join_token)
  }

  # Executors hold leases; let a replacement start before the old task drains.
  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200

  network_configuration {
    subnets          = var.subnet_ids
    security_groups  = local.sg_ids
    assign_public_ip = var.assign_public_ip
  }

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }
}
