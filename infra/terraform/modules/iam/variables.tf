variable "name" {
  description = "Logical IAM name prefix."
  type        = string
}

variable "github_repos" {
  description = "GitHub repos allowed to assume the ECR-push role, in 'owner/repo' form."
  type        = list(string)
  default     = ["lowplane/backend"]
}

variable "eks_oidc_provider_arn" {
  description = "OIDC provider ARN from the eks module. Empty disables the ArgoCD role."
  type        = string
  default     = ""
}

variable "eks_oidc_provider_url" {
  description = "OIDC provider URL from the eks module."
  type        = string
  default     = ""
}
