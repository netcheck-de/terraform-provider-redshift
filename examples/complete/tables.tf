# Table definitions in the consumer-local workspace schema. Terraform owns the definitions, never the rows; real
# deployments should add lifecycle { prevent_destroy = true }, which this disposable example omits so it can be destroyed.
resource "redshift_table" "accounts" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_accounts"

  columns = [
    { name = "account_id", type = "integer", nullable = false },
    { name = "name", type = "varchar(128)", encoding = "ZSTD" },
  ]
  primary_key = ["account_id"]
  diststyle   = "ALL"
}

resource "redshift_table" "orders" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_orders"
  owner    = redshift_user.loader.name

  columns = [
    { name = "order_id", type = "bigint", identity = { seed = 1, step = 1 } },
    { name = "account_id", type = "integer", nullable = false, encoding = "AZ64" },
    { name = "status", type = "varchar(16)", default = "'new'", encoding = "BYTEDICT" },
    { name = "amount", type = "numeric(12,2)", encoding = "AZ64" },
    { name = "created_at", type = "timestamp", nullable = false, default = "getdate()" },
  ]

  primary_key = ["order_id"]
  unique      = [["account_id", "created_at"]]
  foreign_keys = [{
    columns            = ["account_id"]
    references_schema  = redshift_table.accounts.schema
    references_table   = redshift_table.accounts.name
    references_columns = redshift_table.accounts.primary_key
  }]

  distkey = "account_id"
  sortkey = ["created_at", "order_id"]
}

data "redshift_table" "orders" {
  provider = redshift.consumer
  database = redshift_table.orders.database
  schema   = redshift_table.orders.schema
  name     = redshift_table.orders.name
}
