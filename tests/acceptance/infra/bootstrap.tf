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

# Redshift forbids password-disabled superusers. Its documented IAM-only pattern uses a random MD5 hash whose
# plaintext password is unknown, rather than creating a usable password or retrieving the administrator secret.
resource "random_id" "iam_password_hash" {
  byte_length = 16
}

resource "aws_redshiftdata_statement" "iam_password" {
  workgroup_name = aws_redshiftserverless_workgroup.this.workgroup_name
  database       = var.db_name
  secret_arn     = aws_redshiftserverless_namespace.this.admin_password_secret_arn
  sql            = sensitive("ALTER USER \"${replace(coalesce(local.iam_sql_user, "invalid"), "\"", "\"\"")}\" PASSWORD 'md5${random_id.iam_password_hash.hex}'")

  depends_on = [aws_redshiftdata_statement.iam_identity]
}

resource "aws_redshiftdata_statement" "iam_superuser" {
  workgroup_name = aws_redshiftserverless_workgroup.this.workgroup_name
  database       = var.db_name
  secret_arn     = aws_redshiftserverless_namespace.this.admin_password_secret_arn
  sql            = "ALTER USER \"${replace(coalesce(local.iam_sql_user, "invalid"), "\"", "\"\"")}\" CREATEUSER"

  # Enable the unknown password before granting superuser; the IAM session initially creates a password-disabled user.
  depends_on = [aws_redshiftdata_statement.iam_password]
}

# PUBLIC's engine-wide default can include commands not modeled by the released permission resource.
resource "aws_redshiftdata_statement" "public_assumerole" {
  workgroup_name = aws_redshiftserverless_workgroup.this.workgroup_name
  database       = var.db_name
  secret_arn     = aws_redshiftserverless_namespace.this.admin_password_secret_arn
  sql            = "REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR ALL"
}
