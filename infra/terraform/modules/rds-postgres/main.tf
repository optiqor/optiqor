# RDS Postgres 16 module — Multi-AZ + KMS encryption + PITR + cross-region
# snapshot replication. Settings here are the committed Phase 1 DR
# baseline (RPO 5min / RTO 30min).

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

resource "aws_db_subnet_group" "this" {
  name       = "${var.name}-db-subnets"
  subnet_ids = var.subnet_ids
}

resource "aws_security_group" "this" {
  name        = "${var.name}-db"
  description = "Optiqor Postgres ingress"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 5432
    to_port         = 5432
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

# Parameter group enables pg_stat_statements (for prompt-cache &
# query-analytics dashboards) and turns on log_min_duration_statement
# at 500ms so slow queries surface in CloudWatch.
resource "aws_db_parameter_group" "this" {
  name   = "${var.name}-pg16"
  family = "postgres16"

  parameter {
    name  = "shared_preload_libraries"
    value = "pg_stat_statements"
    apply_method = "pending-reboot"
  }
  parameter {
    name  = "log_min_duration_statement"
    value = "500"
  }
  parameter {
    name  = "log_statement"
    value = "ddl"
  }
}

resource "aws_db_instance" "this" {
  identifier        = var.name
  engine            = "postgres"
  engine_version    = "16"
  instance_class    = var.instance_class
  allocated_storage = var.allocated_storage_gb

  db_name           = "optiqor"
  username          = "optiqor_migrator"
  manage_master_user_password = true
  master_user_secret_kms_key_id = var.kms_key_arn

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.this.id]
  parameter_group_name   = aws_db_parameter_group.this.name

  # DR baseline (Phase 1, todo.md production-readiness gap #8)
  multi_az                            = var.multi_az
  backup_retention_period             = 35  # PITR window
  backup_window                       = "03:00-04:00"
  delete_automated_backups            = false
  copy_tags_to_snapshot               = true
  storage_encrypted                   = true
  kms_key_id                          = var.kms_key_arn
  performance_insights_enabled        = true
  performance_insights_kms_key_id     = var.kms_key_arn
  performance_insights_retention_period = 7
  enabled_cloudwatch_logs_exports     = ["postgresql"]
  monitoring_interval                 = 60
  monitoring_role_arn                 = aws_iam_role.rds_monitoring.arn
  deletion_protection                 = var.deletion_protection
  skip_final_snapshot                 = !var.deletion_protection
  auto_minor_version_upgrade          = true
}

resource "aws_iam_role" "rds_monitoring" {
  name = "${var.name}-rds-monitoring"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "monitoring.rds.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "rds_monitoring" {
  role       = aws_iam_role.rds_monitoring.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonRDSEnhancedMonitoringRole"
}
