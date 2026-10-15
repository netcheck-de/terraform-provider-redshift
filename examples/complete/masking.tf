# Dynamic data masking on Terraform-managed tables in the consumer-local workspace schema; Terraform drops them before
# the schema. The provider's admin identity is a superuser, which may manage masking policies.
resource "redshift_table" "customers" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_customers"

  column {
    name = "id"
    type = "INTEGER"
  }

  column {
    name = "email"
    type = "VARCHAR(256)"
  }
}

# Addresses listed here stay readable; the masking expression reads the table through the policy grant below.
resource "redshift_table" "masking_exempt" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_masking_exempt"

  column {
    name = "email"
    type = "VARCHAR(256)"
  }
}

resource "redshift_masking_policy" "email" {
  provider   = redshift.consumer
  database   = redshift_table.masking_exempt.database
  name       = "example_mask_email"
  expression = "CASE WHEN email IN (SELECT email FROM ${redshift_table.masking_exempt.schema}.${redshift_table.masking_exempt.name}) THEN email ELSE REGEXP_REPLACE(email, '^[^@]+', '***') END"

  input_column {
    name = "email"
    type = "VARCHAR(256)"
  }
}

resource "redshift_policy_grant" "masking_lookup" {
  provider      = redshift.consumer
  database_name = redshift_masking_policy.email.database
  schema_name   = redshift_table.masking_exempt.schema
  object_name   = redshift_table.masking_exempt.name
  policy_type   = "MASKING"
  policy_name   = redshift_masking_policy.email.name
  privileges    = ["SELECT"]
}

resource "redshift_masking_policy_attachment" "email_readers" {
  provider     = redshift.consumer
  database     = redshift_masking_policy.email.database
  policy       = redshift_masking_policy.email.name
  schema       = redshift_table.customers.schema
  relation     = redshift_table.customers.name
  columns      = ["email"]
  grantee      = redshift_role.readers.name
  grantee_type = "ROLE"
  priority     = 10

  # The policy reads its lookup table only once the grant exists.
  depends_on = [redshift_policy_grant.masking_lookup]

  # Redshift refuses to drop an attached policy, so a change that replaces the policy must detach it first.
  lifecycle {
    replace_triggered_by = [redshift_masking_policy.email.input_column]
  }
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
