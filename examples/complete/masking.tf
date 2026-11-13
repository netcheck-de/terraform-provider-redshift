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

  lifecycle {
    postcondition {
      condition = try(
        self.definition_fingerprint == redshift_masking_policy.email.definition_fingerprint,
        false,
      )
      error_message = "The masking policy catalog must report the expression fingerprint recorded after creation, without comparing rewritten SQL text."
    }
  }
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

  lifecycle {
    postcondition {
      condition     = try(self.exists && self.priority == 10 && self.input_columns == tolist(["email"]), false)
      error_message = "The catalog must attach the email masking policy to the readers role at priority 10, mapping its input to the email column."
    }
  }
}

data "redshift_masking_policies" "local" {
  provider = redshift.consumer
  database = redshift_masking_policy.email.database
}

data "redshift_masking_policy_attachments" "customers" {
  provider = redshift.consumer
  database = redshift_masking_policy_attachment.email_readers.database
  schema   = redshift_masking_policy_attachment.email_readers.schema
  relation = redshift_masking_policy_attachment.email_readers.relation
}

# Policy SELECT grants have no catalog readback; verification.tf exercises lookup access as the restricted reader.
data "redshift_table" "customers" {
  provider = redshift.consumer
  database = redshift_table.customers.database
  schema   = redshift_table.customers.schema
  name     = redshift_table.customers.name

  lifecycle {
    postcondition {
      condition = try(
        self.owner == redshift_table.customers.owner && length(self.owner) > 0, false,
      )
      error_message = "The masking customers table must exist in the catalog with the owner recorded after creation."
    }
  }
}

data "redshift_table" "masking_exempt" {
  provider = redshift.consumer
  database = redshift_table.masking_exempt.database
  schema   = redshift_table.masking_exempt.schema
  name     = redshift_table.masking_exempt.name

  lifecycle {
    postcondition {
      condition = try(
        self.owner == redshift_table.masking_exempt.owner && length(self.owner) > 0, false,
      )
      error_message = "The masking exemption table must exist in the catalog with the owner recorded after creation."
    }
  }
}
