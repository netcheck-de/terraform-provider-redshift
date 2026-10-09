resource "redshift_identity_provider" "this" {
  name            = "analytics-redshift-idc"
  namespace       = "ncidc"
  application_arn = aws_redshift_idc_application.this.idc_managed_application_arn
  iam_role_arn    = aws_iam_role.idc.arn
}
