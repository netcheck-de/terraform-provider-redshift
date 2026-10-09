resource "redshift_assumerole_grant" "loader" {
  iam_role_arn = "arn:aws:iam::123456789012:role/redshift-data"
  grantee      = redshift_role.loader.name
  grantee_type = "ROLE"
  privileges   = ["COPY", "UNLOAD"]
}
