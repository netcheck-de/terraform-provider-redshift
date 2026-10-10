# Mocked runs for tables. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

# Observed metadata; Terraform cannot mock values for computed nested attribute lists, so columns stay generated.
override_data {
  target = data.redshift_table.orders
  values = {
    id                     = "orders-lookup-identity"
    owner                  = "example_loader"
    primary_key            = { columns = ["order_id"] }
    distribution           = { style = "KEY", key = "account_id" }
    sort_key               = { style = "COMPOUND", columns = ["created_at", "order_id"] }
    effective_distribution = { style = "KEY", key = "account_id", auto = false }
  }
}

# What Redshift chose for the AUTO table, as a read after apply reports it.
override_resource {
  target = redshift_table.events
  values = {
    effective_distribution = { style = "EVEN", key = null, auto = true }
    effective_sort_key     = { style = "NONE", columns = [], auto = true }
  }
}

run "tables_apply" {
  command = apply

  assert {
    condition = (
      redshift_table.accounts.database == redshift_schema.local.database && redshift_table.accounts.schema == redshift_schema.local.name &&
      redshift_table.accounts.primary_key.columns == tolist(["account_id"]) && redshift_table.accounts.distribution.style == "ALL" &&
      redshift_table.orders.schema == redshift_schema.local.name && redshift_table.orders.owner == redshift_user.loader.name &&
      [for column in redshift_table.orders.column : column.name] == ["order_id", "account_id", "status", "amount", "created_at"] &&
      redshift_table.orders.column[0].identity.seed == 1 && redshift_table.orders.column[0].identity.step == 1 &&
      redshift_table.orders.column[1].identity == null && redshift_table.orders.column[2].default == "'new'" &&
      redshift_table.orders.distribution.key == "account_id" && redshift_table.orders.sort_key.columns == tolist(["created_at", "order_id"]) &&
      one(redshift_table.orders.unique).columns == tolist(["account_id", "created_at"])
    )
    error_message = "The example tables must live in the local workspace schema with identity, defaults, keys, and the loader as owner."
  }
  assert {
    condition = (
      one(redshift_table.orders.foreign_key).references.schema == redshift_table.accounts.schema &&
      one(redshift_table.orders.foreign_key).references.table == redshift_table.accounts.name &&
      one(redshift_table.orders.foreign_key).references.columns == redshift_table.accounts.primary_key.columns &&
      one(redshift_table.orders.foreign_key).columns == tolist(["account_id"])
    )
    error_message = "The orders foreign key must reference the accounts primary key, so destroy drops orders first."
  }
  assert {
    condition = (
      redshift_table.events.distribution == null && redshift_table.events.sort_key == null &&
      output.events_table_layout.distribution.style == "EVEN" && output.events_table_layout.distribution.auto &&
      output.events_table_layout.sort_key.style == "NONE" && output.events_table_layout.sort_key.auto
    )
    error_message = "The events table must leave distribution and sort key to Redshift and expose the effective layout."
  }
  assert {
    condition = (
      data.redshift_table.orders.name == redshift_table.orders.name && data.redshift_table.orders.schema == redshift_table.orders.schema &&
      output.orders_table.id == "orders-lookup-identity" && output.orders_table.owner == "example_loader" &&
      length(output.orders_table.columns) == length(data.redshift_table.orders.column) &&
      output.orders_table.primary_key == tolist(["order_id"]) && output.orders_table.distribution.style == "KEY" &&
      output.orders_table.distribution.key == "account_id" && output.orders_table.sort_key.style == "COMPOUND" &&
      output.orders_table.sort_key.columns == tolist(["created_at", "order_id"]) && !output.orders_table.effective_distribution.auto
    )
    error_message = "The table lookup must expose the observed owner, keys, and distribution of the orders table."
  }
}
