# Mocked runs for databases, schemas, and external schemas. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

run "databases_apply" {
  command = apply

  override_data {
    target = data.redshift_database.producer
    values = {
      id               = "producer-database-lookup-identity"
      database_type    = "local"
      owner            = "admin"
      connection_limit = -1
      collation        = "CASE_SENSITIVE"
      isolation_level  = "SNAPSHOT"
      with_permissions = false
    }
  }

  override_data {
    target = data.redshift_schema.local
    values = {
      id    = "schema-lookup-identity"
      owner = "admin"
      quota = 1024
    }
  }

  override_data {
    target = data.redshift_external_schema.shared
    values = {
      id              = "external-schema-lookup-identity"
      source_type     = "REDSHIFT"
      source_database = "example_shared"
      source_schema   = "public"
      owner           = "example_loader"
    }
  }

  assert {
    condition = (
      redshift_database.local.connection_limit == 100 && redshift_database.local.collation == "CASE_SENSITIVE" &&
      redshift_database.local.isolation_level == "SNAPSHOT" && redshift_schema.local.quota == 1024
    )
    error_message = "The consumer-local database and schema must carry the example's options."
  }

  assert {
    condition = (
      redshift_external_schema.shared.source_type == "REDSHIFT" &&
      redshift_external_schema.shared.database == redshift_database.local.name &&
      redshift_external_schema.shared.source_database == redshift_database.shared.name &&
      redshift_external_schema.shared.source_schema == "public" &&
      redshift_external_schema.shared.owner == redshift_user.loader.name &&
      data.redshift_external_schema.shared.name == redshift_external_schema.shared.name
    )
    error_message = "The cross-database external schema must map the shared database into the local database for the loader."
  }

  assert {
    condition = (
      output.database_options.producer_owner == "admin" &&
      output.database_options.producer_collation == "CASE_SENSITIVE" &&
      output.database_options.producer_isolation == "SNAPSHOT" &&
      output.database_options.producer_limit == -1 &&
      output.database_options.local_schema_quota == 1024 &&
      output.shared_external_schema.id == "external-schema-lookup-identity" &&
      output.shared_external_schema.source_type == "REDSHIFT" &&
      output.shared_external_schema.source_database == "example_shared" &&
      output.shared_external_schema.source_schema == "public" &&
      output.shared_external_schema.owner == "example_loader"
    )
    error_message = "Database outputs must expose the lookup values."
  }
}
