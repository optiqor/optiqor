# KMS module — symmetric data-at-rest key + an ECDSA P-256 receipt
# signing key kept around for backwards compatibility. New deployments
# should pick up the signer key from modules/kms-signer (ADR-0017,
# scoped IAM per ADR-0011 §"Signer service isolation").

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

resource "aws_kms_key" "data" {
  description         = "${var.name} data-at-rest encryption (RDS, S3, Secrets Manager)"
  enable_key_rotation = true
  multi_region        = var.multi_region
}

resource "aws_kms_alias" "data" {
  name          = "alias/${var.name}-data"
  target_key_id = aws_kms_key.data.key_id
}

# Receipt signing key — ECDSA P-256 per ADR-0017. Never extractable.
# Yearly rotation handled out-of-band: a new key is created and the
# transparency log records the rollover. Old keys remain enabled for
# verification of historical Receipts. Prefer modules/kms-signer for
# new deployments — that module scopes IAM tighter.
resource "aws_kms_key" "receipt_signing" {
  description              = "${var.name} Receipt ECDSA P-256 signing key (legacy; prefer modules/kms-signer)"
  customer_master_key_spec = "ECC_NIST_P256"
  key_usage                = "SIGN_VERIFY"
  multi_region             = var.multi_region
  enable_key_rotation      = false # asymmetric keys can't auto-rotate; we rotate via new key + tlog
}

resource "aws_kms_alias" "receipt_signing" {
  name          = "alias/${var.name}-receipt-signing"
  target_key_id = aws_kms_key.receipt_signing.key_id
}
