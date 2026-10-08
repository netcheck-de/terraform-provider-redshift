# Basic SQL roles never depend on SSO; adding an instance ARN enables separate group-mapped roles.
resource "redshift_role" "readers" {
  provider = redshift.consumer
  name     = "example_readers"
}

resource "redshift_role" "operators" {
  provider = redshift.consumer
  name     = "example_operators"
}

resource "redshift_role_grant" "operators" {
  provider = redshift.consumer
  role     = "sys:dba"
  to_role  = redshift_role.operators.name
}

# The password source and secret version retain the test user's password in Terraform state.
resource "random_password" "reader" {
  length      = 24
  special     = false
  min_lower   = 1
  min_upper   = 1
  min_numeric = 1
}

resource "redshift_user" "reader" {
  provider    = redshift.consumer
  name        = "example_reader"
  password_wo = random_password.reader.result
}

resource "redshift_role_grant" "reader" {
  provider = redshift.consumer
  role     = redshift_role.readers.name
  to_user  = redshift_user.reader.name
}

resource "redshift_grant" "shared_read" {
  for_each = {
    DATABASE = ["USAGE"]
    SCHEMAS  = ["USAGE"]
    TABLES   = ["SELECT"]
  }
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  role          = redshift_role.readers.name
  scope         = each.key
  privileges    = each.value
}

resource "redshift_grant" "local_schema" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.readers.name
  scope         = "SCHEMA"
  privileges    = ["USAGE"]
}

resource "redshift_group" "readers" {
  provider = redshift.consumer
  name     = "example_readers"
}

resource "redshift_group_membership" "reader" {
  provider = redshift.consumer
  group    = redshift_group.readers.name
  user     = redshift_user.reader.name
}

resource "redshift_object_grant" "group_schema" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["USAGE"]
}

resource "redshift_system_grant" "operators" {
  provider   = redshift.consumer
  role       = redshift_role.operators.name
  privileges = ["CREATE ROLE"]
}

resource "redshift_assumerole_grant" "reader" {
  provider     = redshift.consumer
  count        = var.enable_assumerole_grant ? 1 : 0
  iam_role_arn = "default"
  grantee      = redshift_role.readers.name
  grantee_type = "ROLE"
  privileges   = ["COPY", "UNLOAD"]

  depends_on = [aws_redshiftdata_statement.assumerole_policy]
}

resource "redshift_default_privileges" "reader_tables" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  owner         = redshift_user.reader.name
  object_type   = "TABLES"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["SELECT"]
}

data "redshift_group" "readers" {
  provider = redshift.consumer
  name     = redshift_group.readers.name
}

data "redshift_role" "readers" {
  provider = redshift.consumer
  name     = redshift_role.readers.name
}

data "redshift_user" "reader" {
  provider = redshift.consumer
  name     = redshift_user.reader.name
}

data "redshift_group_membership" "reader" {
  provider = redshift.consumer
  group    = redshift_group_membership.reader.group
  user     = redshift_group_membership.reader.user
}

data "redshift_role_grant" "reader" {
  provider = redshift.consumer
  role     = redshift_role_grant.reader.role
  to_user  = redshift_role_grant.reader.to_user
}

data "redshift_role_grant" "operators" {
  provider = redshift.consumer
  role     = redshift_role_grant.operators.role
  to_role  = redshift_role_grant.operators.to_role
}

data "redshift_grant" "shared_read" {
  for_each      = redshift_grant.shared_read
  provider      = redshift.consumer
  database_name = each.value.database_name
  role          = each.value.role
  scope         = each.value.scope
}

data "redshift_grant" "local_schema" {
  provider      = redshift.consumer
  database_name = redshift_grant.local_schema.database_name
  schema_name   = redshift_grant.local_schema.schema_name
  role          = redshift_grant.local_schema.role
  scope         = redshift_grant.local_schema.scope
}

data "redshift_object_grant" "group_schema" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.group_schema.database_name
  schema_name   = redshift_object_grant.group_schema.schema_name
  object_type   = redshift_object_grant.group_schema.object_type
  grantee       = redshift_object_grant.group_schema.grantee
  grantee_type  = redshift_object_grant.group_schema.grantee_type
}

data "redshift_system_grant" "operators" {
  provider = redshift.consumer
  role     = redshift_system_grant.operators.role
}

data "redshift_assumerole_grant" "reader" {
  provider     = redshift.consumer
  iam_role_arn = var.enable_assumerole_grant ? redshift_assumerole_grant.reader[0].iam_role_arn : "default"
  grantee      = var.enable_assumerole_grant ? redshift_assumerole_grant.reader[0].grantee : redshift_role.readers.name
  grantee_type = "ROLE"
}

data "redshift_default_privileges" "reader_tables" {
  provider      = redshift.consumer
  database_name = redshift_default_privileges.reader_tables.database_name
  owner         = redshift_default_privileges.reader_tables.owner
  object_type   = redshift_default_privileges.reader_tables.object_type
  grantee       = redshift_default_privileges.reader_tables.grantee
  grantee_type  = redshift_default_privileges.reader_tables.grantee_type
}
