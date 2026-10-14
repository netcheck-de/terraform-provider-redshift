# COPY templates and an optional COPY JOB in the consumer-local workspace schema. Terraform drops them, and revokes the
# template grant, before the schema's restrictive DROP SCHEMA and before the loader user they belong to.
variable "copy_job" {
  description = "Optional COPY JOB fixture: an s3:// prefix of CSV files with an id,label header that the consumer IAM role can read, and whether Redshift loads new files automatically, which needs an S3 event integration between the prefix's bucket and the consumer namespace. Null skips the job and its target table."
  type = object({
    source = string
    auto   = optional(bool, false)
  })
  default = null

  validation {
    condition     = var.copy_job == null ? true : startswith(var.copy_job.source, "s3://")
    error_message = "copy_job.source must be an s3:// prefix."
  }
}

resource "redshift_template" "csv_events" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_csv_events"
  format   = "CSV"

  parameters = {
    IGNOREHEADER = "1"
    TIMEFORMAT   = "auto"
    EMPTYASNULL  = ""
    ENCODING     = "UTF8"
  }
}

# MAXERROR is allowed in a template but not in a copy job; the loader owns this template.
resource "redshift_template" "json_events" {
  provider        = redshift.consumer
  database        = redshift_schema.local.database
  schema          = redshift_schema.local.name
  name            = "example_json_events"
  owner           = redshift_user.loader.name
  format          = "JSON"
  format_argument = "auto ignorecase"

  parameters = {
    MAXERROR        = "10"
    TRUNCATECOLUMNS = ""
  }
}

resource "redshift_object_grant" "loader_template" {
  provider      = redshift.consumer
  database_name = redshift_template.csv_events.database
  schema_name   = redshift_template.csv_events.schema
  object_name   = redshift_template.csv_events.name
  object_type   = "TEMPLATE"
  grantee       = redshift_user.loader.name
  grantee_type  = "USER"
  privileges    = ["USAGE"]
}

data "redshift_template" "csv_events" {
  provider = redshift.consumer
  database = redshift_template.csv_events.database
  schema   = redshift_template.csv_events.schema
  name     = redshift_template.csv_events.name
}

data "redshift_templates" "local" {
  provider = redshift.consumer
  database = redshift_template.json_events.database
  schema   = redshift_template.json_events.schema
}

resource "redshift_table" "copy_target" {
  count    = var.copy_job == null ? 0 : 1
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_copy_target"

  column {
    name = "id"
    type = "INTEGER"
  }

  column {
    name = "label"
    type = "VARCHAR(64)"
  }
}

# COPY JOB needs an explicit IAM role; the consumer role is attached to the namespace.
resource "redshift_copy_job" "fixture" {
  count    = var.copy_job == null ? 0 : 1
  provider = redshift.consumer
  database = redshift_table.copy_target[0].database
  name     = "example_fixture_load"
  source   = var.copy_job.source
  iam_role = aws_iam_role.consumer.arn
  format   = "CSV"
  auto     = var.copy_job.auto

  parameters = {
    IGNOREHEADER = "1"
  }

  table {
    schema  = redshift_table.copy_target[0].schema
    name    = redshift_table.copy_target[0].name
    columns = ["id", "label"]
  }
}

data "redshift_copy_job" "fixture" {
  count    = var.copy_job == null ? 0 : 1
  provider = redshift.consumer
  database = redshift_copy_job.fixture[0].database
  name     = redshift_copy_job.fixture[0].name
}

data "redshift_copy_jobs" "local" {
  provider = redshift.consumer
  database = redshift_database.local.name

  depends_on = [redshift_copy_job.fixture]
}
