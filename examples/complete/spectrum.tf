locals {
  # Warehouse policies must precede the external schema and table, so they cannot reference the table resource.
  # Derive catalog/table ARNs from the database's ARN to retain its actual partition, region, and account.
  glue_catalog_arn = "${split(":database/", aws_glue_catalog_database.fixture.arn)[0]}:catalog"
  glue_table_arn   = "${replace(aws_glue_catalog_database.fixture.arn, ":database/", ":table/")}/${local.fixture_table_name}"
}

resource "redshift_external_schema" "glue" {
  provider      = redshift.producer
  database      = redshift_database.producer.name
  name          = "example_external"
  glue_database = aws_glue_catalog_database.fixture.name
  iam_role_arn  = aws_iam_role.producer.arn
  region        = var.region
  # Bump to recreate the mapping after an incompatible Glue catalog change.
  refresh_revision = "1"
}

data "redshift_external_schema" "glue" {
  provider = redshift.producer
  database = redshift_external_schema.glue.database
  name     = redshift_external_schema.glue.name
}

resource "aws_glue_catalog_database" "fixture" {
  provider    = aws.producer
  name        = "${replace(local.name, "-", "_")}_fixture"
  description = "Disposable CSV fixture for Redshift Spectrum"
}

resource "aws_glue_catalog_table" "fixture" {
  provider      = aws.producer
  name          = local.fixture_table_name
  database_name = aws_glue_catalog_database.fixture.name
  table_type    = "EXTERNAL_TABLE"
  parameters = {
    EXTERNAL                 = "TRUE"
    classification           = "csv"
    "skip.header.line.count" = "1"
  }

  # Create the mapping against an empty catalog, then remove this table before restrictive DROP SCHEMA.
  depends_on = [redshift_external_schema.glue]

  storage_descriptor {
    location      = "s3://${module.fixture_bucket.s3_bucket_id}/data/"
    input_format  = "org.apache.hadoop.mapred.TextInputFormat"
    output_format = "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat"

    ser_de_info {
      name                  = "fixture_csv"
      serialization_library = "org.apache.hadoop.hive.serde2.lazy.LazySimpleSerDe"
      parameters = {
        "field.delim"          = ","
        "serialization.format" = ","
      }
    }

    columns {
      name = "id"
      type = "INT"
    }

    columns {
      name = "label"
      type = "VARCHAR(64)"
    }
  }
}

# Cross-account Spectrum needs both the role policy and catalog resource policy.
# This example owns the producer catalog policy when cross-account mode is used.
resource "aws_glue_resource_policy" "fixture" {
  provider = aws.producer
  count    = local.cross_account ? 1 : 0
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = aws_iam_role.consumer.arn }
      Action    = ["glue:GetDatabase", "glue:GetDatabases", "glue:GetTable", "glue:GetTables", "glue:GetPartition", "glue:GetPartitions", "glue:BatchGetPartition"]
      Resource = [
        local.glue_catalog_arn,
        aws_glue_catalog_database.fixture.arn,
        local.glue_table_arn,
      ]
    }]
  })
}

# Redshift writes table and partition definitions through the external schema's role, so the role needs catalog write
# access for the table this example manages in SQL; the Glue fixture table above stays read-only.
resource "aws_iam_role_policy" "producer_spectrum_tables" {
  provider = aws.producer
  name     = "spectrum-tables"
  role     = aws_iam_role.producer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "glue:CreateTable", "glue:UpdateTable", "glue:DeleteTable", "glue:GetTable",
          "glue:CreatePartition", "glue:BatchCreatePartition", "glue:UpdatePartition", "glue:DeletePartition",
          "glue:BatchDeletePartition", "glue:GetPartition", "glue:GetPartitions", "glue:BatchGetPartition",
        ]
        Resource = [
          local.glue_catalog_arn,
          aws_glue_catalog_database.fixture.arn,
          "${replace(aws_glue_catalog_database.fixture.arn, ":database/", ":table/")}/${local.spectrum_table_name}",
        ]
      },
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = "${module.fixture_bucket.s3_bucket_arn}/spectrum/*"
      },
    ]
  })
}

locals {
  spectrum_table_name = "events"
  spectrum_location   = "s3://${module.fixture_bucket.s3_bucket_id}/spectrum/${local.spectrum_table_name}/"
}

# A Spectrum table defined in SQL; Terraform owns its catalog entry, so no aws_glue_catalog_table may manage it too.
resource "redshift_external_table" "events" {
  provider = redshift.producer
  database = redshift_external_schema.glue.database
  schema   = redshift_external_schema.glue.name
  name     = local.spectrum_table_name

  column {
    name = "id"
    type = "INTEGER"
  }

  column {
    name = "label"
    type = "VARCHAR(64)"
  }

  partition_key {
    name = "event_date"
    type = "DATE"
  }

  field_delimiter  = ","
  stored_as        = "TEXTFILE"
  location         = local.spectrum_location
  table_properties = { "skip.header.line.count" = "1" }

  depends_on = [aws_iam_role_policy.producer_spectrum_tables]
}

resource "redshift_external_partition" "events_first_day" {
  provider = redshift.producer
  database = redshift_external_table.events.database
  schema   = redshift_external_table.events.schema
  table    = redshift_external_table.events.name
  values   = { event_date = "2024-01-01" }
  location = "${local.spectrum_location}event_date=2024-01-01/"
}

data "redshift_external_table" "events" {
  provider = redshift.producer
  database = redshift_external_table.events.database
  schema   = redshift_external_table.events.schema
  name     = redshift_external_table.events.name
}

data "redshift_external_partition" "events_first_day" {
  provider = redshift.producer
  database = redshift_external_partition.events_first_day.database
  schema   = redshift_external_partition.events_first_day.schema
  table    = redshift_external_partition.events_first_day.table
  values   = redshift_external_partition.events_first_day.values
}
