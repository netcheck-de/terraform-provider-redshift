locals {
  # Row-level security gets its own fixture table, so turning RLS on never hides rows other examples verify.
  rls_table_name = "example_rls_events"
}

# The fixture lives in the owned database's public schema like the other consumer-local fixtures, so dropping the
# database removes it and no example depends on a managed table resource.
resource "aws_redshiftdata_statement" "rls_table" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_database.local.name
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "CREATE TABLE IF NOT EXISTS public.${local.rls_table_name} (id INTEGER, region VARCHAR(64))"
}

# Readers see only the rows whose region equals their SQL user name.
resource "redshift_rls_policy" "own_region" {
  provider  = redshift.consumer
  database  = redshift_database.local.name
  name      = "example_own_region"
  predicate = "region = current_user"

  column {
    name = "region"
    type = "VARCHAR(64)"
  }
}

resource "redshift_rls_policy_attachment" "readers" {
  provider     = redshift.consumer
  policy       = redshift_rls_policy.own_region.name
  database     = redshift_rls_policy.own_region.database
  schema       = "public"
  relation     = local.rls_table_name
  grantee      = redshift_role.readers.name
  grantee_type = "ROLE"

  depends_on = [aws_redshiftdata_statement.rls_table]

  # Replacing the policy (a WITH change) must detach it first, because DROP RLS POLICY refuses an attached policy.
  lifecycle {
    replace_triggered_by = [redshift_rls_policy.own_region.column, redshift_rls_policy.own_region.alias]
  }
}

# Turning RLS on after the attachment exists means readers are never left with an unfiltered or empty table; destroy
# turns RLS off before the attachment and policy are removed.
resource "redshift_table_security" "rls_events" {
  provider                     = redshift.consumer
  database                     = redshift_rls_policy_attachment.readers.database
  schema                       = redshift_rls_policy_attachment.readers.schema
  relation                     = redshift_rls_policy_attachment.readers.relation
  row_level_security           = true
  conjunction_type             = "AND"
  datashare_row_level_security = true
}

data "redshift_rls_policy" "own_region" {
  provider = redshift.consumer
  database = redshift_rls_policy.own_region.database
  name     = redshift_rls_policy.own_region.name
}

data "redshift_rls_policy_attachment" "readers" {
  provider     = redshift.consumer
  policy       = redshift_rls_policy_attachment.readers.policy
  database     = redshift_rls_policy_attachment.readers.database
  schema       = redshift_rls_policy_attachment.readers.schema
  relation     = redshift_rls_policy_attachment.readers.relation
  grantee      = redshift_rls_policy_attachment.readers.grantee
  grantee_type = redshift_rls_policy_attachment.readers.grantee_type
}

data "redshift_table_security" "rls_events" {
  provider = redshift.consumer
  database = redshift_table_security.rls_events.database
  schema   = redshift_table_security.rls_events.schema
  relation = redshift_table_security.rls_events.relation
}

data "redshift_rls_policies" "local" {
  provider = redshift.consumer
  database = redshift_rls_policy.own_region.database
}
