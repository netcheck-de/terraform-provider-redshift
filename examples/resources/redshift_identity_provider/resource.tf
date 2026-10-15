resource "redshift_identity_provider" "this" {
  name              = "analytics-redshift-idc"
  namespace         = "ncidc"
  application_arn   = aws_redshift_idc_application.this.idc_managed_application_arn
  iam_role_arn      = aws_iam_role.idc.arn
  auto_create_roles = false
}

# Native federation with Microsoft Entra ID. The secret is write-only; bump the version to send a new one.
ephemeral "aws_secretsmanager_secret_version" "entra" {
  secret_id = "redshift/entra-client-secret"
}

resource "redshift_identity_provider" "entra" {
  name                             = "oauth_standard"
  type                             = "AZURE"
  namespace                        = "aad"
  issuer                           = "https://login.microsoftonline.com/e40d4bb2-7670-44ae-bfb8-5db013221d73/v2.0"
  client_id                        = "871c010f-5e61-4fb1-83ac-98610a7e9110"
  audience                         = ["https://analysis.windows.net/powerbi/connector/AmazonRedshift"]
  client_secret_wo                 = ephemeral.aws_secretsmanager_secret_version.entra.secret_string
  client_secret_wo_version         = 1
  auto_create_roles                = true
  auto_create_roles_include_groups = "finance_%"
}
