data "redshift_datashare_privilege" "share_admins" {
  database_name  = "warehouse"
  datashare_name = "analytics"
  grantee_type   = "ROLE"
  grantee        = "share_admins"
}
