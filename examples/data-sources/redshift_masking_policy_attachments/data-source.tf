data "redshift_masking_policy_attachments" "customers" {
  database = "analytics"
  schema   = "sales"
  relation = "customers"
}

output "masked_columns" {
  value = { for attachment in data.redshift_masking_policy_attachments.customers.masking_policy_attachments : "${attachment.policy}/${attachment.grantee}" => attachment.columns }
}
