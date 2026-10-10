# Row-level security on Terraform-managed tables in the consumer-local workspace schema. The policy gets its own table,
# so turning RLS on never hides rows other examples verify; Terraform drops both tables before the schema.
resource "redshift_table" "rls_events" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_rls_events"

  column {
    name = "id"
    type = "integer"
  }
  column {
    name = "region"
    type = "varchar(64)"
  }
}

# Maps SQL users to the regions they may read. The policy reads it through the policy grant below.
resource "redshift_table" "rls_regions" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_rls_regions"

  column {
    name = "reader"
    type = "varchar(128)"
  }
  column {
    name = "region"
    type = "varchar(64)"
  }
}

# Readers see only the rows of the regions the lookup table assigns to their SQL user.
resource "redshift_rls_policy" "own_region" {
  provider  = redshift.consumer
  database  = redshift_table.rls_regions.database
  name      = "example_own_region"
  predicate = "region IN (SELECT region FROM ${redshift_table.rls_regions.schema}.${redshift_table.rls_regions.name} WHERE reader = current_user)"

  column {
    name = "region"
    type = "VARCHAR(64)"
  }
}

# Without SELECT on its lookup table the policy cannot evaluate its predicate, so the grant exists before RLS applies.
resource "redshift_policy_grant" "rls_regions" {
  provider      = redshift.consumer
  database_name = redshift_table.rls_regions.database
  schema_name   = redshift_table.rls_regions.schema
  object_name   = redshift_table.rls_regions.name
  policy_type   = "RLS"
  policy_name   = redshift_rls_policy.own_region.name
  privileges    = ["SELECT"]

  # A replaced policy is a new catalog object without this grant, and Redshift does not report policy grants, so only a
  # replacement of the grant gives it back.
  lifecycle {
    replace_triggered_by = [redshift_rls_policy.own_region.column, redshift_rls_policy.own_region.alias]
  }
}

resource "redshift_rls_policy_attachment" "readers" {
  provider     = redshift.consumer
  policy       = redshift_rls_policy.own_region.name
  database     = redshift_rls_policy.own_region.database
  schema       = redshift_table.rls_events.schema
  relation     = redshift_table.rls_events.name
  grantee      = redshift_role.readers.name
  grantee_type = "ROLE"

  depends_on = [redshift_policy_grant.rls_regions]

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
