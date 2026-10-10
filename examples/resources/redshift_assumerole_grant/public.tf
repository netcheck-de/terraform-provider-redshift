# Turn on identity-specific ASSUMEROLE access control, then let one role use any IAM role for COPY.
resource "redshift_assumerole_grant" "public" {
  iam_role_arn = "ALL"
  grantee      = "public"
  grantee_type = "PUBLIC"
  privileges   = []
}

resource "redshift_assumerole_grant" "loaders_any_role" {
  iam_role_arn = "ALL"
  grantee      = redshift_role.loader.name
  grantee_type = "ROLE"
  privileges   = ["COPY"]

  depends_on = [redshift_assumerole_grant.public]
}
