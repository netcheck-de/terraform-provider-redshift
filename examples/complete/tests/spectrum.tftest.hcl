# Mocked runs for external tables and partitions. The mocks are shared with composition.tftest.hcl through tests/mocks, so
# this block adds runs and assertions here without editing the composition suite.
mock_provider "aws" {
  alias  = "producer"
  source = "./tests/mocks/aws_producer"
}

mock_provider "aws" {
  alias  = "consumer"
  source = "./tests/mocks/aws_consumer"
}

mock_provider "random" {
  source = "./tests/mocks/random"
}

mock_provider "redshift" {
  alias  = "producer"
  source = "./tests/mocks/redshift_producer"
}
mock_provider "redshift" {
  alias  = "consumer"
  source = "./tests/mocks/redshift_consumer"
}
mock_provider "redshift" { alias = "producer_data_api_iam" }
mock_provider "redshift" { alias = "consumer_data_api_iam" }
mock_provider "redshift" { alias = "producer_direct_iam" }
mock_provider "redshift" { alias = "consumer_direct_iam" }
mock_provider "redshift" { alias = "producer_direct_password" }
mock_provider "redshift" { alias = "consumer_direct_password" }

# Give readback fixtures a deterministic S3 location; no AWS provider calls are made.
override_resource {
  target = module.fixture_bucket.aws_s3_bucket.this[0]
  values = { id = "mock-fixture", arn = "arn:aws:s3:::mock-fixture" }
}

override_data {
  target = data.redshift_external_tables.glue
  values = { id = "external-tables-listing-identity" }
}
override_data {
  target = data.redshift_external_partitions.events
  values = { id = "external-partitions-listing-identity" }
}

run "spectrum_scalar_readbacks" {
  command = apply
  # The whole-example runs below still require real nonempty column lists, which Terraform 1.17 cannot mock.
  plan_options { target = [data.redshift_external_schema.glue, data.redshift_external_partition.events_first_day] }
}

run "spectrum_schema_mismatch_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_external_schema.glue] }
  override_data {
    target = data.redshift_external_schema.glue
    values = { source_type = "DATA_CATALOG", glue_database = "wrong_catalog", iam_role_arn = "arn:aws:iam::111111111111:role/producer-fixture", region = "eu-central-1" }
  }
  expect_failures = [data.redshift_external_schema.glue]
}

run "spectrum_missing_columns_block_apply" {
  command = apply
  plan_options { target = [data.redshift_external_table.events] }
  expect_failures = [data.redshift_external_table.events]
}

run "spectrum_partition_mismatch_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_external_partition.events_first_day] }
  override_data {
    target = data.redshift_external_partition.events_first_day
    values = { location = "s3://mock-fixture/spectrum/events/event_date=2024-01-02" }
  }
  expect_failures = [data.redshift_external_partition.events_first_day]
}

run "spectrum_missing_added_column_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_external_table.parquet_events] }
  variables { spectrum_parquet_add_column = true }
  # The generated empty observation is incomplete after the toggle, so the same postcondition must reject it.
  expect_failures = [data.redshift_external_table.parquet_events]
}

run "spectrum_apply" {
  command = apply

  assert {
    condition = (
      redshift_external_table.parquet_events.stored_as == "PARQUET" &&
      [for column in redshift_external_table.parquet_events.column : column.name] == ["id", "label"] &&
      redshift_external_table.parquet_events.schema == redshift_external_schema.glue.name &&
      data.redshift_external_table.parquet_events.name == redshift_external_table.parquet_events.name &&
      output.spectrum_parquet_table.location == "s3://mock-fixture/spectrum/parquet_events/" &&
      output.spectrum_parquet_table.input_format == "org.apache.hadoop.hive.ql.io.parquet.MapredParquetInputFormat" &&
      length(output.spectrum_parquet_table.columns) == length(data.redshift_external_table.parquet_events.column) &&
      contains(jsondecode(aws_iam_role_policy.producer_spectrum_tables.policy).Statement[0].Resource, "arn:aws:glue:eu-central-1:111111111111:table/mock_fixture/parquet_events")
    )
    error_message = "The Parquet table, catalog permission, lookup, and output must be wired to the fixture."
  }

  assert {
    condition = (
      redshift_external_table.events.database == redshift_external_schema.glue.database &&
      redshift_external_table.events.schema == redshift_external_schema.glue.name &&
      redshift_external_table.events.name == "events" &&
      [for column in redshift_external_table.events.column : column.name] == ["id", "label"] &&
      redshift_external_table.events.partition_key[0].name == "event_date" &&
      redshift_external_table.events.stored_as == "TEXTFILE" && redshift_external_table.events.field_delimiter == "," &&
      redshift_external_table.events.location == "s3://${module.fixture_bucket.s3_bucket_id}/spectrum/events/" &&
      redshift_external_table.events.table_properties["skip.header.line.count"] == "1"
    )
    error_message = "The external table must live in the Glue-backed external schema under the fixture bucket's spectrum/ prefix."
  }
  assert {
    condition = (
      redshift_external_partition.events_first_day.table == redshift_external_table.events.name &&
      redshift_external_partition.events_first_day.schema == redshift_external_table.events.schema &&
      redshift_external_partition.events_first_day.values == tomap({ event_date = "2024-01-01" }) &&
      startswith(redshift_external_partition.events_first_day.location, redshift_external_table.events.location)
    )
    error_message = "The partition must target the managed table and a folder below its location."
  }
  assert {
    condition = (
      contains(jsondecode(aws_iam_role_policy.producer_spectrum_tables.policy).Statement[0].Action, "glue:CreateTable") &&
      jsondecode(aws_iam_role_policy.producer_spectrum_tables.policy).Statement[0].Resource[2] == "arn:aws:glue:eu-central-1:111111111111:table/mock_fixture/events" &&
      jsondecode(aws_iam_role_policy.producer_spectrum_tables.policy).Statement[1].Resource == "arn:aws:s3:::mock-fixture/spectrum/*" &&
      aws_iam_role_policy.producer_spectrum_tables.role == aws_iam_role.producer.id
    )
    error_message = "The external schema's role must be able to write the managed table's catalog entry and read its data."
  }
  assert {
    condition = (
      length(output.spectrum_table.columns) == length(data.redshift_external_table.events.column) &&
      length(output.spectrum_table.partition_keys) == length(data.redshift_external_table.events.partition_key) &&
      output.spectrum_table.location == "s3://mock-fixture/spectrum/events/" &&
      output.spectrum_table.input_format == "org.apache.hadoop.mapred.TextInputFormat" &&
      output.spectrum_table.first_partition == "s3://mock-fixture/spectrum/events/event_date=2024-01-01" &&
      data.redshift_external_table.events.name == redshift_external_table.events.name &&
      data.redshift_external_partition.events_first_day.values == redshift_external_partition.events_first_day.values
    )
    error_message = "The Spectrum lookups must be wired to the managed table and partition and exposed as outputs."
  }
  # Terraform mocks generate empty lists for nested-attribute lists and cannot override their elements, so the
  # listing contents are covered by the Go tests; here the listings must target the managed schema and table.
  assert {
    condition = (
      data.redshift_external_tables.glue.id == "external-tables-listing-identity" &&
      data.redshift_external_tables.glue.database == redshift_external_schema.glue.database &&
      data.redshift_external_tables.glue.schema == redshift_external_schema.glue.name &&
      data.redshift_external_partitions.events.id == "external-partitions-listing-identity" &&
      data.redshift_external_partitions.events.schema == redshift_external_table.events.schema &&
      data.redshift_external_partitions.events.table == redshift_external_table.events.name &&
      output.spectrum_listings == { tables = {}, partitions = [] }
    )
    error_message = "The Spectrum listings must cover the managed schema and table and be exposed as outputs."
  }
}

run "spectrum_parquet_insert_column" {
  command = apply

  variables {
    spectrum_parquet_add_column = true
  }

  override_data {
    target = data.redshift_external_table.parquet_events
    values = {
      # A real catalog physically appends amount. Terraform 1.17 cannot mock that nested list.
      location     = "s3://mock-fixture/spectrum/parquet_events/"
      stored_as    = "PARQUET"
      input_format = "org.apache.hadoop.hive.ql.io.parquet.MapredParquetInputFormat"
    }
  }

  assert {
    condition = (
      [for column in redshift_external_table.parquet_events.column : column.name] == ["id", "amount", "label"] &&
      redshift_external_table.parquet_events.column[1].type == "DECIMAL(8,2)" &&
      redshift_external_table.parquet_events.stored_as == "PARQUET"
    )
    error_message = "The toggle must insert the Parquet amount block between the existing columns."
  }
}
