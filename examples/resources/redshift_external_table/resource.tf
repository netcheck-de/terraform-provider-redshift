resource "redshift_external_table" "sales" {
  database = redshift_external_schema.raw.database
  schema   = redshift_external_schema.raw.name
  name     = "sales"

  columns = [
    { name = "sales_id", type = "integer" },
    { name = "price_paid", type = "decimal(8,2)" },
    { name = "sale_time", type = "timestamp" },
  ]
  partition_keys = [
    { name = "sale_date", type = "date" },
  ]

  field_delimiter = "\t"
  stored_as       = "TEXTFILE"
  location        = "s3://example-bucket/tickit/sales/"
  table_properties = {
    "skip.header.line.count" = "1"
    "numRows"                = "172000"
  }
}

resource "redshift_external_table" "events" {
  database = redshift_external_schema.raw.database
  schema   = redshift_external_schema.raw.name
  name     = "events"

  columns = [
    { name = "id", type = "bigint" },
    { name = "payload", type = "varchar(65535)" },
  ]

  serde            = "org.openx.data.jsonserde.JsonSerDe"
  serde_properties = { "strip.outer.array" = "true" }
  stored_as        = "TEXTFILE"
  location         = "s3://example-bucket/events/"
}
