resource "redshift_role" "readers" {
  name = "ncidc:analytics-readers"
}

# Hand the role to a dedicated administrator instead of the creating user.
resource "redshift_role" "auditors" {
  name  = "auditors"
  owner = redshift_user.security_admin.name
}

# A role of a native identity provider carries the group's ID from Microsoft Entra ID.
resource "redshift_role" "entra_finance" {
  name        = "aad:finance"
  external_id = "8c7a2f3e-0d1b-4c5e-9f6a-2b3c4d5e6f70"
}
