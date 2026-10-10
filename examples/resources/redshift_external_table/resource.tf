resource "redshift_external_table" "sales" {
  database = redshift_external_schema.raw.database
  schema   = redshift_external_schema.raw.name
  name     = "sales"

  column {
    name = "sales_id"
    type = "integer"
  }

  column {
    name = "price_paid"
    type = "decimal(8,2)"
  }

  column {
    name = "sale_time"
    type = "timestamp"
  }

  partition_key {
    name = "sale_date"
    type = "date"
  }

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

  column {
    name = "id"
    type = "bigint"
  }

  column {
    name = "payload"
    type = "varchar(65535)"
  }

  serde            = "org.openx.data.jsonserde.JsonSerDe"
  serde_properties = { "strip.outer.array" = "true" }
  stored_as        = "TEXTFILE"
  location         = "s3://example-bucket/events/"
}

# ORC maps columns by name, so a column block can be added or removed anywhere without replacing the table.
resource "redshift_external_table" "clicks" {
  database = redshift_external_schema.raw.database
  schema   = redshift_external_schema.raw.name
  name     = "clicks"

  column {
    name = "click_id"
    type = "bigint"
  }

  column {
    name = "page"
    type = "varchar(1024)"
  }

  column {
    name = "clicked_at"
    type = "timestamp"
  }

  stored_as = "ORC"
  location  = "s3://example-bucket/clicks/"
}
