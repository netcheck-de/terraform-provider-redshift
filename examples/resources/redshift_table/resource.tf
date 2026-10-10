resource "redshift_table" "events" {
  database = redshift_schema.serving.database
  schema   = redshift_schema.serving.name
  name     = "events"
  owner    = redshift_user.loader.name

  column {
    name = "event_id"
    type = "bigint"
    identity {
      seed = 1
      step = 1
    }
  }
  column {
    name     = "account_id"
    type     = "integer"
    nullable = false
    encoding = "AZ64"
  }
  column {
    name     = "kind"
    type     = "varchar(32)"
    default  = "'unknown'"
    encoding = "BYTEDICT"
  }
  column {
    name = "payload"
    type = "super"
  }
  column {
    name     = "created_at"
    type     = "timestamp"
    nullable = false
    default  = "getdate()"
  }

  primary_key {
    columns = ["event_id"]
  }
  unique {
    columns = ["account_id", "created_at"]
  }
  foreign_key {
    columns = ["account_id"]
    references {
      schema  = redshift_schema.serving.name
      table   = "accounts"
      columns = ["account_id"]
    }
  }

  # Omit distribution and sort_key to let Redshift choose (AUTO); effective_distribution and effective_sort_key
  # report what it applies.
  distribution {
    key = "account_id"
  }
  sort_key {
    columns = ["created_at"]
  }

  # Replacing a table drops its rows.
  lifecycle {
    prevent_destroy = true
  }
}
