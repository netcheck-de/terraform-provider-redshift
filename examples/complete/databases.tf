# SQL fixtures live in an owned disposable database; the AWS-created admin database is never managed here.
resource "redshift_database" "producer" {
  provider = redshift.producer
  name     = "example_producer"
}

data "redshift_database" "producer" {
  provider = redshift.producer
  name     = redshift_database.producer.name
}

# The source table is initialized in the database's existing public schema.
# A separately managed populated schema would block restrictive DROP SCHEMA during destroy.
data "redshift_schema" "source" {
  provider = redshift.producer
  database = redshift_database.producer.name
  name     = "public"
}

resource "redshift_database" "local" {
  provider = redshift.consumer
  name     = "example_local"
}

resource "redshift_schema" "local" {
  provider = redshift.consumer
  database = redshift_database.local.name
  name     = "example_schema"
}

data "redshift_schema" "local" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  name     = redshift_schema.local.name
}

resource "redshift_comment" "local_schema" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  object_type   = "SCHEMA"
  object_name   = redshift_schema.local.name
  text          = "Example consumer-local workspace."
}

data "redshift_comment" "local_schema" {
  provider      = redshift.consumer
  database_name = redshift_comment.local_schema.database_name
  object_type   = redshift_comment.local_schema.object_type
  object_name   = redshift_comment.local_schema.object_name
}

resource "redshift_comment" "local_database" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  object_type   = "DATABASE"
  object_name   = redshift_database.local.name
  text          = "Example consumer-local database."
}

resource "redshift_comment" "local_table" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_type   = "TABLE"
  object_name   = local.local_table_name
  text          = "Example events table created by bootstrap SQL."

  depends_on = [aws_redshiftdata_statement.local_table]
}

resource "redshift_comment" "local_column" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_type   = "COLUMN"
  object_name   = local.local_table_name
  column_name   = "label"
  text          = "Human-readable event label."

  depends_on = [aws_redshiftdata_statement.local_table]
}

resource "redshift_comment" "local_view" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_type   = "VIEW"
  object_name   = local.local_view_name
  text          = "Labels projected from the example events table."

  depends_on = [aws_redshiftdata_statement.local_view]
}
