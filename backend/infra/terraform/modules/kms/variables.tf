variable "name" {
  description = "KMS alias prefix."
  type        = string
}

variable "multi_region" {
  description = "Multi-Region key. Required for prod (DR baseline)."
  type        = bool
  default     = true
}
