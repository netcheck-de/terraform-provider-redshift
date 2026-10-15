data "redshift_debug_grant" "producer_admins" {
  database     = "sales_db@finance-catalog"
  grantee      = "123456789012"
  grantee_type = "ACCOUNT"
}

output "producer_admins_see_unredacted_logs" {
  value = data.redshift_debug_grant.producer_admins.exists
}
