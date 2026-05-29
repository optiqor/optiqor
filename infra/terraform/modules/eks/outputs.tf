output "cluster_name" {
  value = aws_eks_cluster.this.name
}

output "cluster_endpoint" {
  value = aws_eks_cluster.this.endpoint
}

output "oidc_provider_arn" {
  value = aws_iam_openid_connect_provider.this.arn
}

output "oidc_provider_url" {
  value = aws_iam_openid_connect_provider.this.url
}

# cluster_security_group_id is the EKS-managed SG that the control plane
# attaches to every node in every managed node group. RDS + ElastiCache
# allow this SG explicitly so the cluster can reach them; nothing else
# can. EKS auto-attaches this SG to all current and future node groups,
# so wiring it once here covers later autoscaling without a TF apply.
output "cluster_security_group_id" {
  value       = aws_eks_cluster.this.vpc_config[0].cluster_security_group_id
  description = "Auto-attached cluster SG; use as the data-plane allow source"
}
