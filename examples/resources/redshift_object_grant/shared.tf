# A consumer database created from a datashare WITH PERMISSIONS.
# Grant database and schema USAGE separately before reading this table.
resource "redshift_object_grant" "shared_events" {
  database_name = "shared_analytics"
  database_type = "shared"
  schema_name   = "reporting"
  object_name   = "events"
  object_type   = "TABLE"
  grantee       = "analyst"
  grantee_type  = "USER"
  privileges    = ["SELECT"]
}

# Every overload of the shared function name is one permission tuple.
resource "redshift_object_grant" "shared_function" {
  database_name           = "shared_analytics"
  database_type           = "shared"
  schema_name             = "reporting"
  object_name             = "f_shared_identity"
  object_type             = "FUNCTION"
  grantee                 = "analyst"
  grantee_type            = "USER"
  privileges              = ["EXECUTE"]
  grant_option_privileges = ["EXECUTE"]
}

# Use another recipient to avoid overlapping the individual shared function tuple above.
resource "redshift_object_grant" "shared_functions_snapshot" {
  database_name = "shared_analytics"
  database_type = "shared"
  schema_name   = "reporting"
  object_type   = "ALL FUNCTIONS"
  grantee       = "operators"
  grantee_type  = "ROLE"
  privileges    = ["EXECUTE"]
}

# Shared procedures use FUNCTION ACL SQL internally but keep their producer routine kind in the resource identity.
resource "redshift_object_grant" "shared_procedure" {
  database_name = "shared_analytics"
  database_type = "shared"
  schema_name   = "reporting"
  object_name   = "sp_shared_refresh"
  object_type   = "PROCEDURE"
  grantee       = "procedure_reader"
  grantee_type  = "ROLE"
  privileges    = ["EXECUTE"]
}
