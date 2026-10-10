resource "redshift_table" "events" {
  database = redshift_schema.serving.database
  schema   = redshift_schema.serving.name
  name     = "events"
  owner    = redshift_user.loader.name

  columns = [
    { name = "event_id", type = "bigint", identity = { seed = 1, step = 1 } },
    { name = "account_id", type = "integer", nullable = false, encoding = "AZ64" },
    { name = "kind", type = "varchar(32)", default = "'unknown'", encoding = "BYTEDICT" },
    { name = "payload", type = "super" },
    { name = "created_at", type = "timestamp", nullable = false, default = "getdate()" },
  ]

  primary_key = ["event_id"]
  unique      = [["account_id", "created_at"]]
  foreign_keys = [{
    columns            = ["account_id"]
    references_schema  = redshift_schema.serving.name
    references_table   = "accounts"
    references_columns = ["account_id"]
  }]

  distkey = "account_id"
  sortkey = ["created_at"]

  # Replacing a table drops its rows.
  lifecycle {
    prevent_destroy = true
  }
}
