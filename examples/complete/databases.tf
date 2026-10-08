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
