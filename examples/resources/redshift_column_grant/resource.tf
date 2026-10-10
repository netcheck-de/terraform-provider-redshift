resource "redshift_column_grant" "events" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "events"
  grantee       = redshift_role.analysts.name
  grantee_type  = "ROLE"
  privileges = {
    SELECT = ["id", "label", "created_at"]
    UPDATE = ["label"]
  }
}
