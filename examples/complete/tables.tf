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

data "redshift_table" "accounts" {
  provider = redshift.consumer
  database = redshift_table.accounts.database
  schema   = redshift_table.accounts.schema
  name     = redshift_table.accounts.name

  lifecycle {
    postcondition {
      condition = try(
        [for column in self.column : column.name] == ["account_id", "name"] &&
        !self.column[0].nullable && self.column[1].encoding == "ZSTD" &&
        self.primary_key.columns == redshift_table.accounts.primary_key.columns && self.distribution.style == "ALL",
        false,
      )
      error_message = "The accounts table must retain its ordered columns, NOT NULL account key, ZSTD name encoding, primary key, and ALL distribution."
    }
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
# Apply normally first, insert any test rows, then apply with -var=table_rename_event_id=true. Keep it true on later
# applies: the retained renamed_from hint is retry-safe and event_id values survive as event_key.
variable "table_rename_event_id" {
  type        = bool
  default     = false
  description = "Opt in to the data-preserving events.event_id to event_key migration after the initial apply."
}

resource "redshift_table" "events" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "example_events"

  column {
    name         = var.table_rename_event_id ? "event_key" : "event_id"
    renamed_from = var.table_rename_event_id ? "event_id" : null
    type         = "BIGINT"
  }

  column {
    name = "payload"
    type = "SUPER"
  }
}

data "redshift_table" "events" {
  provider = redshift.consumer
  database = redshift_table.events.database
  schema   = redshift_table.events.schema
  name     = redshift_table.events.name

  lifecycle {
    postcondition {
      condition = try(
        [for column in self.column : column.name] == [for column in redshift_table.events.column : column.name] &&
        [for column in self.column : column.type] == ["BIGINT", "SUPER"] &&
        alltrue([for column in self.column : column.renamed_from == null]) &&
        self.effective_distribution.auto && self.effective_sort_key.auto,
        false,
      )
      error_message = "The events catalog must show exactly the current event_id/event_key and payload columns, no stored rename hint, and AUTO layout after creation or migration."
    }
  }
}

data "redshift_table" "orders" {
  provider = redshift.consumer
  database = redshift_table.orders.database
  schema   = redshift_table.orders.schema
  name     = redshift_table.orders.name

  lifecycle {
    postcondition {
      condition = try(
        self.owner == redshift_table.orders.owner &&
        [for column in self.column : column.name] == [for column in redshift_table.orders.column : column.name] &&
        self.primary_key.columns == redshift_table.orders.primary_key.columns &&
        self.distribution.key == redshift_table.orders.distribution.key &&
        self.sort_key.columns == redshift_table.orders.sort_key.columns &&
        self.column[0].identity.seed == 1 && self.column[0].identity.step == 1 &&
        !self.column[1].nullable && !self.column[4].nullable &&
        self.column[1].encoding == "AZ64" && self.column[2].encoding == "BYTEDICT" && self.column[3].encoding == "AZ64" &&
        toset([for key in self.unique : join(",", key.columns)]) == toset(["account_id,created_at"]) &&
        length(self.foreign_key) == 1 &&
        one(self.foreign_key).columns == one(redshift_table.orders.foreign_key).columns &&
        one(self.foreign_key).references.schema == redshift_table.accounts.schema &&
        one(self.foreign_key).references.table == redshift_table.accounts.name &&
        one(self.foreign_key).references.columns == redshift_table.accounts.primary_key.columns,
        false,
      )
      error_message = "The orders table must report its owner, ordered columns, identity, nullability, explicit encodings, primary/unique/foreign keys, distribution key, and sort key."
    }
  }
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

data "redshift_comment" "orders_key" {
  provider        = redshift.consumer
  database_name   = redshift_comment.orders_key.database_name
  object_type     = redshift_comment.orders_key.object_type
  schema_name     = redshift_comment.orders_key.schema_name
  object_name     = redshift_comment.orders_key.object_name
  constraint_name = redshift_comment.orders_key.constraint_name

  lifecycle {
    postcondition {
      condition     = self.text == redshift_comment.orders_key.text
      error_message = "The orders primary-key constraint must retain its configured comment."
    }
  }
}
