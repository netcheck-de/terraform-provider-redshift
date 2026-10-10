resource "redshift_datashare_privilege" "share_admins" {
  database_name  = redshift_datashare.analytics.database
  datashare_name = redshift_datashare.analytics.name
  grantee_type   = "ROLE"
  grantee        = "share_admins"
  privileges     = ["ALTER", "SHARE"]
}
