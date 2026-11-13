data "redshift_rls_policy_attachments" "orders" {
  database = "analytics"
  schema   = "sales"
  relation = "orders"
}

output "rls_policy_recipients" {
  value = [for attachment in data.redshift_rls_policy_attachments.orders.rls_policy_attachments : "${attachment.policy} -> ${attachment.grantee_type} ${attachment.grantee}"]
}
