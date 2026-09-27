variable "name" {
  description = "Replication group identifier."
  type        = string
}

variable "vpc_id" {
  description = "VPC for the security group."
  type        = string
}

variable "subnet_ids" {
  description = "Cache subnets."
  type        = list(string)
}

variable "allow_security_group_ids" {
  description = "Source security groups permitted to reach :6379."
  type        = list(string)
}

variable "kms_key_arn" {
  description = "KMS key for at-rest encryption + AUTH token secret."
  type        = string
}

variable "node_type" {
  description = "Cache node instance type."
  type        = string
  default     = "cache.t4g.medium"
}

variable "num_cache_clusters" {
  description = "Replicas. 1 in dev; ≥2 in prod for failover."
  type        = number
  default     = 2
}
