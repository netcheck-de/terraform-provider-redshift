resource "redshift_system_grant" "operators" {
  role       = redshift_role.operators.name
  privileges = ["CREATE ROLE", "CREATE DATASHARE"]
}
