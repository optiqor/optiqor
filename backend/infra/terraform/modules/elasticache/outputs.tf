output "primary_endpoint" {
  value = aws_elasticache_replication_group.this.primary_endpoint_address
}

output "auth_secret_arn" {
  value = aws_secretsmanager_secret.auth.arn
}
