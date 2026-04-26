# KMS module — multi-region key for Receipt signing (HSM-backed
# Ed25519 SIGN_VERIFY key) plus a separate symmetric key for
# at-rest encryption.

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

# Receipt signing key — asymmetric SIGN_VERIFY, never extractable.
# Yearly rotation handled out-of-band: a new key is created and the
# transparency log records the rollover. Old keys remain enabled for
# verification of historical Receipts.
resource "aws_kms_key" "receipt_signing" {
  description              = "${var.name} Receipt Ed25519 signing key"
  customer_master_key_spec = "ECC_NIST_P256"
  key_usage                = "SIGN_VERIFY"
  multi_region             = var.multi_region
  enable_key_rotation      = false # asymmetric keys can't auto-rotate; we rotate via new key + tlog
}

resource "aws_kms_alias" "receipt_signing" {
  name          = "alias/${var.name}-receipt-signing"
  target_key_id = aws_kms_key.receipt_signing.key_id
}
