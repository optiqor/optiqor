output "key_id" {
  description = "KMS key ID. Use as OPTIQOR_RECEIPT_SIGNING_KEY_ID for the signer-mode boot."
  value       = aws_kms_key.receipt_signing.key_id
}

output "key_arn" {
  description = "Full key ARN. IAM policies attach to this ARN, not the alias."
  value       = aws_kms_key.receipt_signing.arn
}

output "alias_name" {
  description = "Key alias including the ECDSA-P-256 algorithm tag. The Go signer (internal/receipts/kms.NewSigner) rejects mis-tagged keyIDs."
  value       = aws_kms_alias.receipt_signing.name
}
