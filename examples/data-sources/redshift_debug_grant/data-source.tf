data "redshift_debug_grant" "producer_admins" {
  # The provider must connect to the warehouse hosting this producer-local federated database.
  database     = "sales_db"
  grantee      = "123456789012"
  grantee_type = "ACCOUNT"
}

output "producer_admins_see_unredacted_logs" {
  value = data.redshift_debug_grant.producer_admins.exists
}
