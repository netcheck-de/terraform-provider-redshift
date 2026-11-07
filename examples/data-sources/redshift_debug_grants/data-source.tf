data "redshift_debug_grants" "sales" {
  database = "sales_db"
}

output "debug_grantees" {
  value = [for grant in data.redshift_debug_grants.sales.debug_grants : "${grant.grantee_type} ${grant.grantee}"]
}
