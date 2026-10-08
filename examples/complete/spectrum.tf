locals {
  fixture_table_name = "fixture"

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
      type = "int"
    }

    columns {
      name = "label"
      type = "varchar(64)"
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
