# Column-level access on the consumer-local fixtures. The readers role and group hold no table-level SELECT or UPDATE
# on these relations, which would otherwise supersede the column grants.
resource "redshift_column_grant" "reader_events" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_name   = local.local_table_name
  grantee       = redshift_role.readers.name
  grantee_type  = "ROLE"
  privileges = {
    SELECT = ["id", "label"]
    UPDATE = ["label"]
  }

  depends_on = [aws_redshiftdata_statement.local_table]
}

# Views accept only column-level SELECT.
resource "redshift_column_grant" "group_event_labels" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_name   = local.local_view_name
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges = {
    SELECT = ["label"]
  }

  depends_on = [aws_redshiftdata_statement.local_view]
}

# The readers group sees every column of the managed orders table except the amount, including columns added later.
# It holds no table-level SELECT on the table, which would cover every column.
resource "redshift_column_grant" "group_orders" {
  provider      = redshift.consumer
  database_name = redshift_table.orders.database
  schema_name   = redshift_table.orders.schema
  object_name   = redshift_table.orders.name
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges = {
    SELECT = [for column in redshift_table.orders.column : column.name if column.name != "amount"]
  }
}

# Operators may create stored procedures in the local database.
resource "redshift_language_grant" "operator_procedures" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  language_name = "plpgsql"
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["USAGE"]
}

# The loader may create SQL UDFs and pass that right on; only users can hold grant options.
resource "redshift_language_grant" "loader_sql" {
  provider                = redshift.consumer
  database_name           = redshift_database.local.name
  language_name           = "sql"
  grantee                 = redshift_user.loader.name
  grantee_type            = "USER"
  privileges              = ["USAGE"]
  grant_option_privileges = ["USAGE"]
}

data "redshift_column_grant" "reader_events" {
  provider      = redshift.consumer
  database_name = redshift_column_grant.reader_events.database_name
  schema_name   = redshift_column_grant.reader_events.schema_name
  object_name   = redshift_column_grant.reader_events.object_name
  grantee       = redshift_column_grant.reader_events.grantee
  grantee_type  = redshift_column_grant.reader_events.grantee_type
}

data "redshift_language_grant" "loader_sql" {
  provider      = redshift.consumer
  database_name = redshift_language_grant.loader_sql.database_name
  language_name = redshift_language_grant.loader_sql.language_name
  grantee       = redshift_language_grant.loader_sql.grantee
  grantee_type  = redshift_language_grant.loader_sql.grantee_type
}

# Every grant on the local table, after the table-level and column-level grants exist.
data "redshift_grants" "local_events" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  object_type   = "TABLE"
  schema_name   = "public"
  object_name   = local.local_table_name

  depends_on = [redshift_object_grant.loader_events, redshift_column_grant.reader_events]
}

# Every grant the loader holds in the local database.
data "redshift_grants" "loader" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  grantee       = redshift_user.loader.name
  grantee_type  = "USER"

  depends_on = [redshift_object_grant.loader_events, redshift_language_grant.loader_sql]
}

data "redshift_column_grants" "local" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"

  depends_on = [redshift_column_grant.reader_events, redshift_column_grant.group_event_labels]
}
