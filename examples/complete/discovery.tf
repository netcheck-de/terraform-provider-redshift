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

  depends_on = [redshift_database.local, redshift_database.zero_etl]

  lifecycle {
    postcondition {
      condition     = contains([for database in self.databases : database.name if upper(database.database_type) == "LOCAL"], redshift_database.local.name)
      error_message = "Local database discovery must include the managed local workspace database."
    }
  }
}

data "redshift_schemas" "local" {
  provider = redshift.consumer
  database = redshift_schema.local.database

  depends_on = [redshift_schema.local]

  lifecycle {
    postcondition {
      condition     = anytrue([for schema in self.schemas : schema.name == redshift_schema.local.name && schema.quota == redshift_schema.local.quota])
      error_message = "Schema discovery must include the managed workspace schema with its quota."
    }
  }
}

data "redshift_tables" "fixture" {
  provider = redshift.producer
  database = redshift_database.producer.name
  schema   = data.redshift_schema.source.name

  depends_on = [aws_redshiftdata_statement.source_table]

  lifecycle {
    postcondition {
      condition     = anytrue([for table in self.tables : table.name == local.fixture_table_name && table.schema == data.redshift_schema.source.name])
      error_message = "Producer table discovery must find the bootstrap fixture in public."
    }
  }
}

data "redshift_columns" "fixture" {
  provider = redshift.producer
  database = redshift_database.producer.name
  schema   = data.redshift_schema.source.name
  table    = local.fixture_table_name

  depends_on = [aws_redshiftdata_statement.source_table]

  lifecycle {
    postcondition {
      condition = (
        [for column in self.columns : column.name] == ["id", "label"] &&
        [for column in self.columns : column.ordinal_position] == [1, 2] &&
        [for column in self.columns : column.data_type] == ["INTEGER", "CHARACTER VARYING"]
      )
      error_message = "Producer column discovery must find the fixture's ordered INTEGER id and VARCHAR label columns."
    }
  }
}

data "redshift_constraints" "discovery_keys" {
  provider        = redshift.consumer
  database        = redshift_database.local.name
  schema          = redshift_comment.discovery_key.schema_name
  table           = redshift_comment.discovery_key.object_name
  constraint_type = "PRIMARY KEY"

  lifecycle {
    postcondition {
      condition = anytrue([for key in self.constraints :
        key.name == redshift_comment.discovery_key.constraint_name && key.constraint_type == "PRIMARY KEY" &&
        key.schema == redshift_comment.discovery_key.schema_name && key.table == redshift_comment.discovery_key.object_name &&
        key.columns == tolist(["id"])
      ])
      error_message = "Constraint discovery must find the bootstrap keyed table's named primary key on id."
    }
  }
}

data "redshift_comment" "discovery_key" {
  provider        = redshift.consumer
  database_name   = redshift_comment.discovery_key.database_name
  object_type     = redshift_comment.discovery_key.object_type
  schema_name     = redshift_comment.discovery_key.schema_name
  object_name     = redshift_comment.discovery_key.object_name
  constraint_name = redshift_comment.discovery_key.constraint_name

  lifecycle {
    postcondition {
      condition     = self.text == redshift_comment.discovery_key.text
      error_message = "The discovered primary-key constraint must retain its configured comment."
    }
  }
}
