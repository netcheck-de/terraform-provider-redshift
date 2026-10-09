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

# Scoped grants inside one schema. FUNCTIONS and PROCEDURES share one Redshift catalog scope, so each
# role/schema tuple uses only one of them.
resource "redshift_grant" "local_schema_tables" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.readers.name
  scope         = "TABLES"
  privileges    = ["SELECT"]
}

resource "redshift_grant" "local_schema_functions" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.readers.name
  scope         = "FUNCTIONS"
  privileges    = ["EXECUTE"]
}

resource "redshift_grant" "local_schema_procedures" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.operators.name
  scope         = "PROCEDURES"
  privileges    = ["EXECUTE"]
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

resource "redshift_object_grant" "loader_events" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_name   = local.local_table_name
  object_type   = "TABLE"
  grantee       = redshift_user.loader.name
  grantee_type  = "USER"
  privileges    = ["SELECT", "INSERT"]

  depends_on = [aws_redshiftdata_statement.local_table]
}

resource "redshift_object_grant" "operators_database" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  object_type   = "DATABASE"
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["TEMPORARY"]
}

resource "redshift_object_grant" "public_schema_usage" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee       = "public"
  grantee_type  = "PUBLIC"
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

# An explicit IAM role ARN can be granted to a single user in addition to the role-wide default grant.
resource "redshift_assumerole_grant" "loader" {
  provider     = redshift.consumer
  count        = var.enable_assumerole_grant ? 1 : 0
  iam_role_arn = aws_iam_role.consumer.arn
  grantee      = redshift_user.loader.name
  grantee_type = "USER"
  privileges   = ["COPY"]

  depends_on = [aws_redshiftdata_statement.assumerole_policy]
}

# Schema-scoped default privileges require the owning user to hold CREATE on that schema.
resource "redshift_object_grant" "loader_schema" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee       = redshift_user.loader.name
  grantee_type  = "USER"
  privileges    = ["CREATE", "USAGE"]
}

resource "redshift_default_privileges" "reader_schema_tables" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  owner         = redshift_user.loader.name
  object_type   = "TABLES"
  grantee       = redshift_role.readers.name
  grantee_type  = "ROLE"
  privileges    = ["SELECT"]

  depends_on = [redshift_object_grant.loader_schema]
}

resource "redshift_default_privileges" "operator_routines" {
  for_each      = toset(["FUNCTIONS", "PROCEDURES"])
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  owner         = redshift_user.loader.name
  object_type   = each.key
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["EXECUTE"]
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
