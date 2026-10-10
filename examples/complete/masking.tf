# Dynamic data masking on block-owned fixtures in the consumer-local database's public schema, so dropping the
# database removes the tables. The provider's admin identity is a superuser, which may manage masking policies.
locals {
  masking_table_name  = "example_customers"
  masking_lookup_name = "example_masking_exempt"
}

resource "aws_redshiftdata_statement" "masking_table" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_database.local.name
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "CREATE TABLE IF NOT EXISTS public.${local.masking_table_name} (id INTEGER, email VARCHAR(256))"
}

# Addresses listed here stay readable; the masking expression reads the table through the policy grant below.
resource "aws_redshiftdata_statement" "masking_lookup" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_database.local.name
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "CREATE TABLE IF NOT EXISTS public.${local.masking_lookup_name} (email VARCHAR(256))"
}

resource "redshift_masking_policy" "email" {
  provider = redshift.consumer
  database = redshift_database.local.name
  name     = "example_mask_email"

  input_columns = [
    { name = "email", type = "VARCHAR(256)" },
  ]
  expression = "CASE WHEN email IN (SELECT email FROM public.${local.masking_lookup_name}) THEN email ELSE REGEXP_REPLACE(email, '^[^@]+', '***') END"

  depends_on = [aws_redshiftdata_statement.masking_lookup]
}

resource "redshift_policy_grant" "masking_lookup" {
  provider      = redshift.consumer
  database_name = redshift_masking_policy.email.database
  schema_name   = "public"
  object_name   = local.masking_lookup_name
  policy_type   = "MASKING"
  policy_name   = redshift_masking_policy.email.name
  privileges    = ["SELECT"]
}

resource "redshift_masking_policy_attachment" "email_readers" {
  provider     = redshift.consumer
  database     = redshift_masking_policy.email.database
  policy       = redshift_masking_policy.email.name
  schema       = "public"
  relation     = local.masking_table_name
  columns      = ["email"]
  grantee      = redshift_role.readers.name
  grantee_type = "ROLE"
  priority     = 10

  # The policy reads its lookup table only once the grant exists.
  depends_on = [aws_redshiftdata_statement.masking_table, redshift_policy_grant.masking_lookup]
}

data "redshift_masking_policy" "email" {
  provider = redshift.consumer
  database = redshift_masking_policy.email.database
  name     = redshift_masking_policy.email.name
}

data "redshift_masking_policy_attachment" "email_readers" {
  provider     = redshift.consumer
  database     = redshift_masking_policy_attachment.email_readers.database
  policy       = redshift_masking_policy_attachment.email_readers.policy
  schema       = redshift_masking_policy_attachment.email_readers.schema
  relation     = redshift_masking_policy_attachment.email_readers.relation
  columns      = redshift_masking_policy_attachment.email_readers.columns
  grantee      = redshift_masking_policy_attachment.email_readers.grantee
  grantee_type = redshift_masking_policy_attachment.email_readers.grantee_type
}

data "redshift_masking_policies" "local" {
  provider = redshift.consumer
  database = redshift_masking_policy.email.database
}
