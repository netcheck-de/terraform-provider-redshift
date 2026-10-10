# Stop granting EXECUTE on new functions of the loader to everyone, then grant it to one group.
resource "redshift_default_privileges" "no_public_functions" {
  database_name = "analytics"
  owner         = redshift_user.loader.name
  object_type   = "FUNCTIONS"
  grantee       = "public"
  grantee_type  = "PUBLIC"
  privileges    = []
}

resource "redshift_default_privileges" "developer_functions" {
  database_name = "analytics"
  owner         = redshift_user.loader.name
  object_type   = "FUNCTIONS"
  grantee       = redshift_group.developers.name
  grantee_type  = "GROUP"
  privileges    = ["EXECUTE"]
}
