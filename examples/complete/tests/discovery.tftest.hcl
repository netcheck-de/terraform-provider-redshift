# Mocked runs for catalog collection lookups and comments. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

run "discovery_apply" {
  command = apply

  assert {
    condition = (
      data.redshift_databases.example.database_type == "local" && data.redshift_databases.example.name_like == "example_%" &&
      data.redshift_schemas.local.database == redshift_database.local.name && data.redshift_schemas.local.schema_type == "local" &&
      data.redshift_tables.fixture.database == redshift_database.producer.name && data.redshift_tables.fixture.schema == "public" &&
      data.redshift_tables.fixture.table_type == "TABLE" &&
      data.redshift_columns.fixture.table == "fixture" && data.redshift_columns.fixture.schema == "public" &&
      data.redshift_constraints.discovery_keys.database == redshift_database.local.name &&
      data.redshift_constraints.discovery_keys.table == "example_discovery_keys" &&
      data.redshift_constraints.discovery_keys.constraint_type == "PRIMARY KEY"
    )
    error_message = "Listings must be filtered to the example's own databases, schema, fixture table, and keyed table."
  }
  assert {
    condition = (
      redshift_comment.discovery_key.object_type == "CONSTRAINT" &&
      redshift_comment.discovery_key.database_name == redshift_database.local.name &&
      redshift_comment.discovery_key.schema_name == "public" &&
      redshift_comment.discovery_key.object_name == "example_discovery_keys" &&
      redshift_comment.discovery_key.constraint_name == "example_discovery_keys_pkey" &&
      redshift_comment.discovery_key.column_name == null &&
      strcontains(aws_redshiftdata_statement.discovery_keys.sql, "CONSTRAINT example_discovery_keys_pkey PRIMARY KEY (id)") &&
      aws_redshiftdata_statement.discovery_keys.database == redshift_database.local.name
    )
    error_message = "The constraint comment must target the block-owned keyed table in the consumer-local database."
  }
  # Terraform mocks generate empty lists for nested-attribute lists and cannot override their elements, so the
  # listing contents are covered by the Go tests; here the output must keep its shape over empty listings.
  assert {
    condition = (
      output.discovery.local_databases == [] && output.discovery.local_schemas == [] &&
      output.discovery.fixture_tables == [] && length(output.discovery.fixture_columns) == 0 &&
      length(output.discovery.primary_keys) == 0 &&
      output.discovery.key_comment == "Example primary key found by redshift_constraints."
    )
    error_message = "The discovery output must expose every listing and the constraint comment."
  }
}
