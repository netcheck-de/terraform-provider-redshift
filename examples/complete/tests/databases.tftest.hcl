# Mocked runs for databases, schemas, external schemas, and their listings. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

mock_provider "redshift" {
  alias  = "producer"
  source = "./tests/mocks/redshift_producer"
}
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
      quota = 2048
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

  # Keep the external-schema listing identity independent from the direct lookup.
  override_data {
    target = data.redshift_external_schemas.local
    values = { id = "external-schemas-listing-identity" }
  }

  assert {
    condition = (
      redshift_database.local.connection_limit == 100 && redshift_database.local.collation == "CASE_SENSITIVE" &&
      redshift_database.local.isolation_level == "SNAPSHOT" && redshift_schema.local.quota == 2048
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
      output.database_options.local_schema_quota == 2048 &&
      output.shared_external_schema.id == "external-schema-lookup-identity" &&
      output.shared_external_schema.source_type == "REDSHIFT" &&
      output.shared_external_schema.source_database == "example_shared" &&
      output.shared_external_schema.source_schema == "public" &&
      output.shared_external_schema.owner == "example_loader"
    )
    error_message = "Database outputs must expose the lookup values."
  }

  assert {
    condition = (
      data.redshift_external_schemas.local.database == redshift_database.local.name &&
      data.redshift_comments.local.database_name == redshift_database.local.name &&
      data.redshift_comments.local.object_type == null &&
      data.redshift_external_schemas.local.id == "external-schemas-listing-identity" &&
      data.redshift_comments.local.id == "comments-listing-identity" &&
      length(output.database_listings.external_schemas) == 0 && length(output.database_listings.comments) == 5
    )
    error_message = "Database listings must cover the local database and expose the listed external schemas and comments."
  }
}

run "database_readback_rejects_wrong_options" {
  command = apply
  override_data {
    target = data.redshift_database.local
    values = { database_type = "LOCAL", connection_limit = 99, collation = "CASE_SENSITIVE", isolation_level = "SNAPSHOT" }
  }
  expect_failures = [data.redshift_database.local]
}

run "external_schema_readback_rejects_wrong_source" {
  command = apply
  override_data {
    target = data.redshift_external_schema.shared
    values = { source_type = "REDSHIFT", source_database = "wrong_database", source_schema = "public", owner = "example_loader" }
  }
  expect_failures = [data.redshift_external_schema.shared]
}

run "database_readback_rejects_stale_collation" {
  command = apply
  variables {
    database_collation_insensitive = true
  }
  expect_failures = [data.redshift_database.collation_probe]
}

run "comments_readback_rejects_missing_annotations" {
  command = apply
  override_data {
    target = data.redshift_comments.local
    values = { comments = [] }
  }
  expect_failures = [data.redshift_comments.local]
}

run "database_collation_migration" {
  command = apply

  variables {
    database_collation_insensitive = true
  }

  override_data {
    target = data.redshift_database.collation_probe
    values = {
      id        = "collation-probe-stable-identity"
      name      = "example_collation_probe"
      collation = "CASE_INSENSITIVE"
    }
  }

  assert {
    condition = (
      redshift_database.collation_probe.collation == "CASE_INSENSITIVE" &&
      output.database_collation_migration.id == "collation-probe-stable-identity" &&
      output.database_collation_migration.name == redshift_database.collation_probe.name &&
      output.database_collation_migration.collation == "CASE_INSENSITIVE"
    )
    error_message = "The empty collation probe must expose the selected collation through its lookup and output."
  }
}
