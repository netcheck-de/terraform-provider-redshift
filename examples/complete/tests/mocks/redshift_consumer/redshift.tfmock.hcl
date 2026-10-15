# Mocking bypasses the framework's plan-time defaults.
mock_resource "redshift_database" {
  defaults = { with_permissions = true }
}
