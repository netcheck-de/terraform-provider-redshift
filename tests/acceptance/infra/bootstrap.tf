# CI authenticates through an assumed IAM role. Redshift Serverless names that SQL identity IAMR:<role-name>.
locals {
  iam_role_name = try(regex(":assumed-role/([^/]+)/", data.aws_caller_identity.fixture.arn)[0], null)
  iam_sql_user  = local.iam_role_name == null ? null : "IAMR:${local.iam_role_name}"
}

# Opening an IAM Data API session creates the SQL identity before the administrator elevates it.
resource "aws_redshiftdata_statement" "iam_identity" {
  workgroup_name = aws_redshiftserverless_workgroup.this.workgroup_name
  database       = var.db_name
  sql            = "SELECT current_user"

  lifecycle {
    precondition {
      condition     = local.iam_sql_user != null
      error_message = "Acceptance provisioning requires assumed-role AWS credentials, as supplied by CI OIDC."
    }
  }
}

resource "aws_redshiftdata_statement" "iam_superuser" {
  workgroup_name = aws_redshiftserverless_workgroup.this.workgroup_name
  database       = var.db_name
  secret_arn     = aws_redshiftserverless_namespace.this.admin_password_secret_arn
  sql            = "ALTER USER \"${replace(coalesce(local.iam_sql_user, "invalid"), "\"", "\"\"")}\" CREATEUSER"

  # The SQL role name does not reference the statement that first creates the IAM identity.
  depends_on = [aws_redshiftdata_statement.iam_identity]
}

# PUBLIC's engine-wide default can include commands not modeled by the released permission resource.
resource "aws_redshiftdata_statement" "public_assumerole" {
  workgroup_name = aws_redshiftserverless_workgroup.this.workgroup_name
  database       = var.db_name
  secret_arn     = aws_redshiftserverless_namespace.this.admin_password_secret_arn
  sql            = "REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR ALL"
}
