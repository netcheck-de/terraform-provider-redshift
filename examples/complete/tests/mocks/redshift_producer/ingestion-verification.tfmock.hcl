# Target-specific readbacks avoid changing unrelated external-schema/table lookups.
# Terraform 1.17 cannot populate computed nested lists (columns and partition keys).
# Leave them empty rather than supplying invalid mock values or bypassing postconditions.
override_data {
  target = data.redshift_external_schema.glue
  values = {
    source_type   = "DATA_CATALOG"
    glue_database = "redshift_oss_e2e_deadbeef_fixture"
    iam_role_arn  = "arn:aws:iam::111111111111:role/producer-fixture"
    region        = "eu-central-1"
  }
}

override_data {
  target = data.redshift_external_table.events
  values = {
    location         = "s3://mock-fixture/spectrum/events/"
    stored_as        = "TEXTFILE"
    field_delimiter  = ","
    input_format     = "org.apache.hadoop.mapred.TextInputFormat"
    table_properties = { "skip.header.line.count" = "1", EXTERNAL = "TRUE" }
  }
}

override_data {
  target = data.redshift_external_table.parquet_events
  values = {
    location     = "s3://mock-fixture/spectrum/parquet_events/"
    stored_as    = "PARQUET"
    input_format = "org.apache.hadoop.hive.ql.io.parquet.MapredParquetInputFormat"
  }
}

override_data {
  target = data.redshift_external_partition.events_first_day
  values = { location = "s3://mock-fixture/spectrum/events/event_date=2024-01-01" }
}
