# Stored procedure authors in the analytics database.
resource "redshift_language_grant" "procedures" {
  database_name = "analytics"
  language_name = "PLPGSQL"
  grantee       = redshift_role.developers.name
  grantee_type  = "ROLE"
  privileges    = ["USAGE"]
}

# A user who may create SQL UDFs and pass that right on.
resource "redshift_language_grant" "sql_udfs" {
  database_name           = "analytics"
  language_name           = "SQL"
  grantee                 = redshift_user.udf_owner.name
  grantee_type            = "USER"
  privileges              = ["USAGE"]
  grant_option_privileges = ["USAGE"]
}

# Only the grantees above may create stored procedures: this revokes the built-in USAGE that PUBLIC holds.
resource "redshift_language_grant" "public_procedures" {
  database_name = "analytics"
  language_name = "PLPGSQL"
  grantee       = "public"
  grantee_type  = "PUBLIC"
  privileges    = []
}
