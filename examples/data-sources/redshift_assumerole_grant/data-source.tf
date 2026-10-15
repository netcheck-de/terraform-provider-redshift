data "redshift_assumerole_grant" "reader" {
  iam_role_arn = "DEFAULT"
  grantee      = "report_readers"
  grantee_type = "ROLE"
}
