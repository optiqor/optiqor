variable "name" {
  description = "Environment-scoped prefix (e.g. optiqor-prod). Becomes part of the alias and tags."
  type        = string
}

variable "multi_region" {
  description = "Replicate the signing key across regions. Set true in prod (EU + US tenants); false in dev/staging."
  type        = bool
  default     = false
}

variable "account_id" {
  description = "AWS account ID hosting the key — used to scope the root-admin clause in the key policy."
  type        = string
}

variable "signer_role_arns" {
  description = "IAM role ARNs allowed to call kms:Sign. Should contain exactly the signer-mode ServiceAccount role (ADR-0011) — keep this list small."
  type        = list(string)
  default     = []
}

variable "verifier_role_arns" {
  description = "IAM role ARNs allowed kms:GetPublicKey + kms:Verify. Includes the verifier workload + any third-party auditor account principals."
  type        = list(string)
  default     = []
}

variable "tags" {
  description = "Tags merged into the KMS key. Project/Environment/Tenant/ManagedBy default tags arrive from the env's default_tags block."
  type        = map(string)
  default     = {}
}
