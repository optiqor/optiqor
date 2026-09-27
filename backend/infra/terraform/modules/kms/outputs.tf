output "data_key_arn" {
  value = aws_kms_key.data.arn
}

output "data_key_alias" {
  value = aws_kms_alias.data.name
}

output "receipt_signing_key_arn" {
  value = aws_kms_key.receipt_signing.arn
}

output "receipt_signing_key_alias" {
  value = aws_kms_alias.receipt_signing.name
}
