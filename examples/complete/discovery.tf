# Catalog discovery over the example's own fixtures: the owned databases, the managed local schema, the producer's
# public.fixture table, and a block-owned keyed table whose primary key carries a comment.

locals {
  discovery_table_name = "example_discovery_keys"
  discovery_key_name   = "example_discovery_keys_pkey"
}

# Like the other consumer-local fixtures, the keyed table lives in the owned database's public schema, so dropping that
# database removes it and the managed example_schema stays empty for restrictive DROP SCHEMA.
resource "aws_redshiftdata_statement" "discovery_keys" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_database.local.name
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "CREATE TABLE IF NOT EXISTS public.${local.discovery_table_name} (id INTEGER NOT NULL, label VARCHAR(64), CONSTRAINT ${local.discovery_key_name} PRIMARY KEY (id))"
}

resource "redshift_comment" "discovery_key" {
  provider        = redshift.consumer
  database_name   = redshift_database.local.name
  object_type     = "CONSTRAINT"
  schema_name     = "public"
  object_name     = local.discovery_table_name
  constraint_name = local.discovery_key_name
  text            = "Example primary key found by redshift_constraints."

  depends_on = [aws_redshiftdata_statement.discovery_keys]
}

data "redshift_databases" "example" {
  provider      = redshift.consumer
  database_type = "local"
  name_like     = "example_%"

  depends_on = [redshift_database.local]
}

data "redshift_schemas" "local" {
  provider    = redshift.consumer
  database    = redshift_schema.local.database
  schema_type = "local"

  depends_on = [redshift_schema.local]
}

data "redshift_tables" "fixture" {
  provider   = redshift.producer
  database   = redshift_database.producer.name
  schema     = data.redshift_schema.source.name
  table_type = "TABLE"

  depends_on = [aws_redshiftdata_statement.source_table]
}

data "redshift_columns" "fixture" {
  provider = redshift.producer
  database = redshift_database.producer.name
  schema   = data.redshift_schema.source.name
  table    = local.fixture_table_name

  depends_on = [aws_redshiftdata_statement.source_table]
}

data "redshift_constraints" "discovery_keys" {
  provider        = redshift.consumer
  database        = redshift_database.local.name
  schema          = redshift_comment.discovery_key.schema_name
  table           = redshift_comment.discovery_key.object_name
  constraint_type = "PRIMARY KEY"
}
