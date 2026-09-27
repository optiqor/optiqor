variable "name" {
  description = "EKS cluster name."
  type        = string
}

variable "kubernetes_version" {
  description = "Kubernetes version. Optiqor tracks N-1 from the latest EKS-supported."
  type        = string
  default     = "1.31"
}

variable "subnet_ids" {
  description = "Subnets to place the cluster ENIs and worker nodes in. Use private subnets in prod."
  type        = list(string)
}

variable "kms_key_arn" {
  description = "KMS key ARN for envelope encryption of Kubernetes Secrets."
  type        = string
}

variable "public_endpoint" {
  description = "Whether the EKS API endpoint is reachable from the public internet."
  type        = bool
  default     = false
}
