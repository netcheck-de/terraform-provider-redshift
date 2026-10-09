data "redshift_assumerole_grant" "reader" {
  iam_role_arn = "default"
  grantee      = "report_readers"
  grantee_type = "ROLE"
}
