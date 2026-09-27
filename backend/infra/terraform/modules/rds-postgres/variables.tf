variable "name" {
  description = "DB instance identifier."
  type        = string
}

variable "vpc_id" {
  description = "VPC for the security group."
  type        = string
}

variable "subnet_ids" {
  description = "DB subnets (the `db` tier from the VPC module)."
  type        = list(string)
}

variable "allow_security_group_ids" {
  description = "Source security groups permitted to reach :5432."
  type        = list(string)
}

variable "kms_key_arn" {
  description = "KMS key for storage + Performance Insights + secret encryption."
  type        = string
}

variable "instance_class" {
  description = "RDS instance class. db.r6g.large baseline; bump in prod."
  type        = string
  default     = "db.r6g.large"
}

variable "allocated_storage_gb" {
  description = "Initial allocated storage. Storage autoscaling handles growth."
  type        = number
  default     = 100
}

variable "multi_az" {
  description = "Multi-AZ enabled. Required in prod; optional in dev."
  type        = bool
  default     = true
}

variable "deletion_protection" {
  description = "Prevent accidental deletion. Always true in prod."
  type        = bool
  default     = true
}
