# kms-signer module — isolated Receipt-signing key.
#
# Lives in its own module (separate from modules/kms) so the signer
# ServiceAccount's IAM policy can reference its outputs without
# pulling in the data-at-rest key — ADR-0011 §"Signer service
# isolation" requires the signer workload's IAM to grant kms:Sign on
# *only* this key alias, no other KMS resource.
#
# Algorithm fixed per ADR-0017: ECDSA on NIST P-256 with SHA-256.
# AWS KMS does not support Ed25519 native signing; the algorithm
# choice is reflected in the key alias suffix (-ecdsa-p256) so the
# Go signer (internal/receipts/kms.NewSigner) refuses mis-tagged keyIDs.

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

resource "aws_kms_key" "receipt_signing" {
  description              = "${var.name} Receipt signing — ECDSA P-256, SIGN_VERIFY only"
  customer_master_key_spec = "ECC_NIST_P256"
  key_usage                = "SIGN_VERIFY"

  # multi_region = true in prod so EU + US tenants verify with their
  # regional replica without crossing the data-residency boundary.
  multi_region = var.multi_region

  # Asymmetric keys can't auto-rotate; rotation policy is "create a
  # new key, record the rollover in the transparency log, keep old
  # keys ENABLED for verification of historical Receipts".
  enable_key_rotation = false

  # Deletion window doubles as a tripwire — 30 days gives the on-call
  # rotation time to verify no historical Receipts depend on the key.
  deletion_window_in_days = 30

  policy = data.aws_iam_policy_document.signer_key.json

  tags = merge(var.tags, {
    Component = "receipt-signer"
    Algorithm = "ecdsa-p256-sha256"
  })
}

# Key alias must encode the algorithm tag — the Go signer asserts on it.
# Renaming the alias requires a coordinated Go-side keyID rotation.
resource "aws_kms_alias" "receipt_signing" {
  name          = "alias/${var.name}-receipt-signing-ecdsa-p256"
  target_key_id = aws_kms_key.receipt_signing.key_id
}

# Key policy — only the signer role gets kms:Sign; only the verifier
# (and any third-party auditor) gets kms:GetPublicKey + kms:Verify.
# Root account retains administrative access for key rotation.
data "aws_iam_policy_document" "signer_key" {
  # Root account: full administration. Required for Terraform plan/apply
  # against the key, plus rotation operations.
  statement {
    sid     = "RootAdmin"
    actions = ["kms:*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.account_id}:root"]
    }
    resources = ["*"]
  }

  # Signer workload: Sign + GetPublicKey only. Cannot decrypt anything,
  # cannot export the private key (AWS refuses), cannot disable the key.
  dynamic "statement" {
    for_each = length(var.signer_role_arns) > 0 ? [1] : []
    content {
      sid     = "SignerSign"
      actions = ["kms:Sign", "kms:GetPublicKey", "kms:DescribeKey"]

      principals {
        type        = "AWS"
        identifiers = var.signer_role_arns
      }
      resources = ["*"]
    }
  }

  # Verifier workload + third-party auditors: read-only verification.
  dynamic "statement" {
    for_each = length(var.verifier_role_arns) > 0 ? [1] : []
    content {
      sid     = "VerifierRead"
      actions = ["kms:GetPublicKey", "kms:Verify", "kms:DescribeKey"]

      principals {
        type        = "AWS"
        identifiers = var.verifier_role_arns
      }
      resources = ["*"]
    }
  }
}
