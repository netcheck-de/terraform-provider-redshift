# Table definitions in the consumer-local workspace schema. Terraform owns the definitions, never the rows; real
# deployments should add lifecycle { prevent_destroy = true }, which this disposable example omits so it can be destroyed.
resource "redshift_table" "accounts" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_accounts"

  column {
    name     = "account_id"
    type     = "INTEGER"
    nullable = false
  }

  column {
    name     = "name"
    type     = "VARCHAR(128)"
    encoding = "ZSTD"
  }

  primary_key {
    columns = ["account_id"]
  }

  distribution {
    style = "ALL"
  }
}

resource "redshift_table" "orders" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_orders"
  owner    = redshift_user.loader.name

  column {
    name = "order_id"
    type = "BIGINT"
    identity {
      seed = 1
      step = 1
    }
  }

  column {
    name     = "account_id"
    type     = "INTEGER"
    nullable = false
    encoding = "AZ64"
  }

  column {
    name     = "status"
    type     = "VARCHAR(16)"
    default  = "'new'"
    encoding = "BYTEDICT"
  }

  column {
    name     = "amount"
    type     = "NUMERIC(12,2)"
    encoding = "AZ64"
  }

  column {
    name     = "created_at"
    type     = "TIMESTAMP"
    nullable = false
    default  = "GETDATE()"
  }

  primary_key {
    columns = ["order_id"]
  }

  unique {
    columns = ["account_id", "created_at"]
  }

  foreign_key {
    columns = ["account_id"]
    references {
      schema  = redshift_table.accounts.schema
      table   = redshift_table.accounts.name
      columns = redshift_table.accounts.primary_key.columns
    }
  }

  distribution {
    key = "account_id"
  }

  sort_key {
    columns = ["created_at", "order_id"]
  }
}

# Distribution and sort key are left to Redshift: without the blocks both are AUTO, and effective_distribution and
# effective_sort_key report what Redshift chose.
resource "redshift_table" "events" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_events"

  column {
    name = "event_id"
    type = "BIGINT"
  }

  column {
    name = "payload"
    type = "SUPER"
  }
}

data "redshift_table" "orders" {
  provider = redshift.consumer
  database = redshift_table.orders.database
  schema   = redshift_table.orders.schema
  name     = redshift_table.orders.name
}

# Redshift names an unnamed primary key <table>_pkey. The comment owns only the annotation; the key stays with the table.
resource "redshift_comment" "orders_key" {
  provider        = redshift.consumer
  database_name   = redshift_table.orders.database
  object_type     = "CONSTRAINT"
  schema_name     = redshift_table.orders.schema
  object_name     = redshift_table.orders.name
  constraint_name = "${redshift_table.orders.name}_pkey"
  text            = "Surrogate key of an example order."
}
