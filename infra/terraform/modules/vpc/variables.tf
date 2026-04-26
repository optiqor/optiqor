variable "name" {
  description = "Logical name prefix used in resource Name tags."
  type        = string
}

variable "cidr" {
  description = "VPC CIDR block. /16 recommended for 3-AZ multi-tier subnetting."
  type        = string
  default     = "10.0.0.0/16"
}

variable "azs" {
  description = "Availability zones. Multi-AZ in prod; single-AZ acceptable in dev."
  type        = list(string)
}
