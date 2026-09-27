variable "name" {
  description = "Bucket name. Must be globally unique."
  type        = string
}

variable "kms_key_arn" {
  description = "KMS key for SSE-KMS encryption."
  type        = string
}

variable "expire_noncurrent_after_days" {
  description = "Days after which noncurrent versions expire. Set to 0 to keep forever."
  type        = number
  default     = 90
}

variable "replication_destination_bucket_arn" {
  description = "Destination bucket ARN for Cross-Region Replication. Empty disables CRR."
  type        = string
  default     = ""
}

variable "replication_destination_kms_key_arn" {
  description = "Destination-region KMS key for replica encryption."
  type        = string
  default     = ""
}
