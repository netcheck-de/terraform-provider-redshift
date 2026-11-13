# COPY templates and a COPY JOB in the consumer-local workspace schema. Terraform drops them, and revokes the
# template grant, before the schema's restrictive DROP SCHEMA and before the loader user they belong to.
variable "copy_job" {
  description = "COPY JOB for the fixture bucket's data/ prefix, created by default without automatic loading. COPY JOB needs an S3 event integration between the bucket and the consumer namespace even without AUTO, so the example creates one. Null skips the job, its target table, and the integration."
  type = object({
    auto = optional(bool, false)
  })
  default = { auto = false }
}

locals {
  copy_job_enabled = var.copy_job != null
}

# Redshift resolves a COPY JOB's bucket to an S3 event integration into the warehouse. The bucket's account creates it,
# and the bucket policy and the consumer namespace's resource policy authorize it.
resource "aws_redshift_integration" "copy_job" {
  provider         = aws.producer
  count            = local.copy_job_enabled ? 1 : 0
  integration_name = "${local.name}-copy"
  source_arn       = module.fixture_bucket.s3_bucket_arn
  target_arn       = aws_redshiftserverless_namespace.consumer.arn

  # Concurrent integration creation can make the shared target temporarily inactive. Wait only for any enabled
  # zero-ETL integration, not for unrelated warehouse SQL resources.
  depends_on = [
    module.fixture_bucket,
    aws_redshift_resource_policy.inbound_integrations,
    aws_redshiftserverless_workgroup.consumer,
    aws_redshift_integration.zero_etl,
    aws_rds_integration.zero_etl,
  ]
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

# A reusable spatial ingestion definition; this creates no load or geometry data.
resource "redshift_template" "shapefile" {
  provider        = redshift.consumer
  database        = redshift_schema.local.database
  schema          = redshift_schema.local.name
  name            = "example_shapefile"
  format          = "SHAPEFILE"
  format_argument = "SIMPLIFY AUTO 0.000001"
}

data "redshift_template" "shapefile" {
  provider = redshift.consumer
  database = redshift_template.shapefile.database
  schema   = redshift_template.shapefile.schema
  name     = redshift_template.shapefile.name

  lifecycle {
    postcondition {
      condition = try(
        self.format == redshift_template.shapefile.format &&
        self.format_argument == redshift_template.shapefile.format_argument &&
        length(coalesce(self.parameters, {})) == 0,
        false,
      )
      error_message = "The SHAPEFILE template readback must report its format and SIMPLIFY argument without extra parameters."
    }
  }
}

# Database-wide listing observes catalog-visible rows without probing unrelated schemas.
data "redshift_templates" "database" {
  provider = redshift.consumer
  database = redshift_schema.local.database

  depends_on = [redshift_template.csv_events, redshift_template.json_events, redshift_template.shapefile]
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

  lifecycle {
    postcondition {
      condition = try(
        self.format == redshift_template.csv_events.format &&
        coalesce(self.format_argument, " ") == coalesce(redshift_template.csv_events.format_argument, " ") &&
        self.parameters == redshift_template.csv_events.parameters,
        false,
      )
      error_message = "The CSV template readback must report its format and every configured COPY parameter."
    }
  }
}

data "redshift_template" "json_events" {
  provider = redshift.consumer
  database = redshift_template.json_events.database
  schema   = redshift_template.json_events.schema
  name     = redshift_template.json_events.name

  lifecycle {
    postcondition {
      condition = try(
        self.owner == redshift_template.json_events.owner &&
        self.format == redshift_template.json_events.format &&
        self.format_argument == redshift_template.json_events.format_argument &&
        self.parameters == redshift_template.json_events.parameters,
        false,
      )
      error_message = "The JSON template readback must report the loader owner, format argument, and COPY parameters."
    }
  }
}

data "redshift_object_grant" "loader_template" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.loader_template.database_name
  schema_name   = redshift_object_grant.loader_template.schema_name
  object_name   = redshift_object_grant.loader_template.object_name
  object_type   = redshift_object_grant.loader_template.object_type
  grantee       = redshift_object_grant.loader_template.grantee
  grantee_type  = redshift_object_grant.loader_template.grantee_type

  lifecycle {
    postcondition {
      condition     = self.privileges == redshift_object_grant.loader_template.privileges
      error_message = "The loader must have the managed USAGE privilege on the CSV template in the catalog."
    }
  }
}

data "redshift_templates" "local" {
  provider = redshift.consumer
  database = redshift_template.json_events.database
  schema   = redshift_template.json_events.schema
}

resource "redshift_table" "copy_target" {
  count    = local.copy_job_enabled ? 1 : 0
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

data "redshift_table" "copy_target" {
  count    = local.copy_job_enabled ? 1 : 0
  provider = redshift.consumer
  database = redshift_table.copy_target[0].database
  schema   = redshift_table.copy_target[0].schema
  name     = redshift_table.copy_target[0].name

  lifecycle {
    postcondition {
      condition = try(
        [for column in self.column : {
          name = column.name
          type = lookup(local.ingestion_type_aliases, upper(replace(column.type, " ", "")), upper(replace(column.type, " ", "")))
          }] == [for column in redshift_table.copy_target[0].column : {
          name = column.name
          type = lookup(local.ingestion_type_aliases, upper(replace(column.type, " ", "")), upper(replace(column.type, " ", "")))
        }],
        false,
      )
      error_message = "The COPY target catalog must report the configured ordered column names and types."
    }
  }
}

# COPY JOB needs an explicit IAM role; the consumer role is attached to the namespace.
resource "redshift_copy_job" "fixture" {
  count    = local.copy_job_enabled ? 1 : 0
  provider = redshift.consumer
  database = redshift_table.copy_target[0].database
  name     = "example_fixture_load"
  source   = "s3://${module.fixture_bucket.s3_bucket_id}/data/"
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

  depends_on = [aws_redshift_integration.copy_job]
}

data "redshift_copy_job" "fixture" {
  count    = local.copy_job_enabled ? 1 : 0
  provider = redshift.consumer
  database = redshift_copy_job.fixture[0].database
  name     = redshift_copy_job.fixture[0].name

  lifecycle {
    postcondition {
      condition = try(
        self.auto == redshift_copy_job.fixture[0].auto &&
        self.iam_role == redshift_copy_job.fixture[0].iam_role &&
        self.source == redshift_copy_job.fixture[0].source &&
        self.format == redshift_copy_job.fixture[0].format &&
        coalesce(self.format_argument, " ") == coalesce(redshift_copy_job.fixture[0].format_argument, " ") &&
        self.parameters == redshift_copy_job.fixture[0].parameters &&
        self.table.schema == redshift_copy_job.fixture[0].table.schema &&
        self.table.name == redshift_copy_job.fixture[0].table.name &&
        self.table.columns == redshift_copy_job.fixture[0].table.columns,
        false,
      )
      error_message = "The COPY JOB readback must match AUTO, IAM role, source, format, parameters, and target definition."
    }
  }
}

data "redshift_copy_jobs" "local" {
  provider = redshift.consumer
  database = redshift_database.local.name

  depends_on = [redshift_copy_job.fixture]
}
