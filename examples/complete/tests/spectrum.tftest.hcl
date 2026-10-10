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

mock_provider "redshift" { alias = "producer" }
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

# Observed values deliberately differ from configuration where the catalog reports its own form. Terraform cannot mock
# lists of nested objects, so the column lists keep their generated values.
override_data {
  target = data.redshift_external_table.events
  values = {
    location     = "s3://mock-fixture/spectrum/events/"
    input_format = "org.apache.hadoop.mapred.TextInputFormat"
  }
}
override_data {
  target = data.redshift_external_partition.events_first_day
  values = { location = "s3://mock-fixture/spectrum/events/event_date=2024-01-01" }
}

run "spectrum_apply" {
  command = apply

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
}
