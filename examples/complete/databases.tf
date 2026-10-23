# SQL fixtures live in an owned disposable database; the AWS-created admin database is never managed here.
variable "database_collation_insensitive" {
  description = "Switch the deliberately empty collation probe database from CASE_SENSITIVE to CASE_INSENSITIVE in place. Keep all database objects out of this probe: Redshift refuses COLLATE when user tables exist."
  type        = bool
  default     = false
}

# An empty database makes the collation migration reproducible without touching the example's populated databases.
resource "redshift_database" "collation_probe" {
  provider  = redshift.consumer
  name      = "example_collation_probe"
  collation = var.database_collation_insensitive ? "CASE_INSENSITIVE" : "CASE_SENSITIVE"
}

data "redshift_database" "collation_probe" {
  provider = redshift.consumer
  name     = redshift_database.collation_probe.name
}

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

# The options spell out Redshift's defaults except the connection limit, so the example behaves like a plain database.
# Default privileges stay in the database and depend on their owners and grantees; the kept PUBLIC FUNCTIONS
# revocation even outlives its resource. Dropping the database before these identities lets DROP USER and DROP ROLE
# succeed on destroy.
resource "redshift_database" "local" {
  provider         = redshift.consumer
  name             = "example_local"
  connection_limit = 100
  collation        = "CASE_SENSITIVE"
  isolation_level  = "SNAPSHOT"

  depends_on = [redshift_user.loader, redshift_user.reader, redshift_role.operators, redshift_role.readers]
}

# Setting a schema quota requires a superuser, which the example's admin connection is. The quota is in megabytes.
resource "redshift_schema" "local" {
  provider = redshift.consumer
  database = redshift_database.local.name
  name     = "example_schema"
  quota    = 2048
}

# A cross-database external schema exposes the consumer's datashare database inside the local database. The loader owns
# it, so the owner is transferred after creation.
resource "redshift_external_schema" "shared" {
  provider        = redshift.consumer
  database        = redshift_database.local.name
  name            = "example_shared_sales"
  source_type     = "REDSHIFT"
  source_database = redshift_database.shared.name
  source_schema   = "public"
  owner           = redshift_user.loader.name
}

data "redshift_external_schema" "shared" {
  provider = redshift.consumer
  database = redshift_external_schema.shared.database
  name     = redshift_external_schema.shared.name
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
