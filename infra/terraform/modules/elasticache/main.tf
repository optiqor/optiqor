# ElastiCache Redis 7 — encryption at rest + in transit, AUTH token in
# Secrets Manager, single-AZ in dev / multi-AZ in prod.

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.name}-cache-subnets"
  subnet_ids = var.subnet_ids
}

resource "aws_security_group" "this" {
  name        = "${var.name}-cache"
  description = "Sevro Redis ingress"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = var.allow_security_group_ids
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "random_password" "auth" {
  length  = 32
  special = false
}

resource "aws_secretsmanager_secret" "auth" {
  name       = "${var.name}-redis-auth"
  kms_key_id = var.kms_key_arn
}

resource "aws_secretsmanager_secret_version" "auth" {
  secret_id     = aws_secretsmanager_secret.auth.id
  secret_string = random_password.auth.result
}

resource "aws_elasticache_replication_group" "this" {
  replication_group_id        = var.name
  description                 = "Sevro Redis ${var.name}"
  engine                      = "redis"
  engine_version              = "7.1"
  node_type                   = var.node_type
  num_cache_clusters          = var.num_cache_clusters
  automatic_failover_enabled  = var.num_cache_clusters > 1
  multi_az_enabled            = var.num_cache_clusters > 1
  port                        = 6379
  subnet_group_name           = aws_elasticache_subnet_group.this.name
  security_group_ids          = [aws_security_group.this.id]
  parameter_group_name        = "default.redis7"
  at_rest_encryption_enabled  = true
  transit_encryption_enabled  = true
  kms_key_id                  = var.kms_key_arn
  auth_token                  = random_password.auth.result
  snapshot_retention_limit    = 7
  snapshot_window             = "03:00-05:00"
  apply_immediately           = false
}
