# Mocked runs for COPY templates, the template grant, and the default copy job. The mocks are shared with
# composition.tftest.hcl through tests/mocks, so this block adds runs and assertions here without editing the composition
# suite.
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

override_resource {
  target = module.fixture_bucket.aws_s3_bucket.this[0]
  values = { id = "mock-fixture", arn = "arn:aws:s3:::mock-fixture" }
}
override_data {
  target = data.redshift_templates.local
  values = { id = "templates-listing-identity" }
}
override_data {
  target = data.redshift_copy_jobs.local
  values = { id = "copy-jobs-listing-identity" }
}

override_data {
  target = data.redshift_templates.database
  values = { id = "database-templates-listing-identity" }
}

run "copy_scalar_readbacks" {
  command = apply
  # Computed nested lists cannot be populated by Terraform 1.17 mocks. Exercise all supported readbacks
  # independently; the whole-example runs below retain the table-column postcondition and expose that limitation.
  plan_options {
    target = [data.redshift_template.csv_events, data.redshift_template.json_events, data.redshift_template.shapefile, data.redshift_object_grant.loader_template, data.redshift_copy_job.fixture]
  }
}

run "copy_auto_readback_update" {
  command = apply
  plan_options { target = [data.redshift_copy_job.fixture] }
  variables { copy_job = { auto = true } }
  override_data {
    target = data.redshift_copy_job.fixture
    values = {
      auto            = true
      iam_role        = "arn:aws:iam::111111111111:role/consumer-fixture"
      source          = "s3://mock-fixture/data/"
      format          = "CSV"
      format_argument = null
      parameters      = { IGNOREHEADER = "1" }
      table           = { schema = "example_schema", name = "example_copy_target", columns = ["id", "label"] }
    }
  }
}

run "csv_parameters_mismatch_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_template.csv_events] }
  override_data {
    target = data.redshift_template.csv_events
    values = { format = "CSV", format_argument = null, parameters = { IGNOREHEADER = "2" } }
  }
  expect_failures = [data.redshift_template.csv_events]
}

run "json_owner_mismatch_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_template.json_events] }
  override_data {
    target = data.redshift_template.json_events
    values = { owner = "wrong_owner", format = "JSON", format_argument = "auto ignorecase", parameters = { MAXERROR = "10", TRUNCATECOLUMNS = "" } }
  }
  expect_failures = [data.redshift_template.json_events]
}

run "shapefile_argument_mismatch_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_template.shapefile] }
  override_data {
    target = data.redshift_template.shapefile
    values = { format = "SHAPEFILE", format_argument = "SIMPLIFY AUTO 0.1", parameters = {} }
  }
  expect_failures = [data.redshift_template.shapefile]
}

run "template_grant_mismatch_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_object_grant.loader_template] }
  override_data {
    target = data.redshift_object_grant.loader_template
    values = { privileges = [] }
  }
  expect_failures = [data.redshift_object_grant.loader_template]
}

run "copy_target_missing_columns_block_apply" {
  command = apply
  plan_options { target = [data.redshift_table.copy_target] }
  expect_failures = [data.redshift_table.copy_target]
}

run "copy_job_definition_mismatch_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_copy_job.fixture] }
  override_data {
    target = data.redshift_copy_job.fixture
    values = {
      auto            = false
      iam_role        = "arn:aws:iam::111111111111:role/consumer-fixture"
      source          = "s3://wrong-bucket/data/"
      format          = "CSV"
      format_argument = null
      parameters      = { IGNOREHEADER = "1" }
      table           = { schema = "example_schema", name = "example_copy_target", columns = ["id", "label"] }
    }
  }
  expect_failures = [data.redshift_copy_job.fixture]
}

run "copy_job_stale_auto_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_copy_job.fixture] }
  variables { copy_job = { auto = true } }
  expect_failures = [data.redshift_copy_job.fixture]
}

run "copy_templates" {
  command = apply
  variables { copy_job = null }

  assert {
    condition = (
      redshift_template.shapefile.format == "SHAPEFILE" &&
      redshift_template.shapefile.format_argument == "SIMPLIFY AUTO 0.000001" &&
      output.copy_checks.shapefile_format == "SHAPEFILE" &&
      output.copy_checks.shapefile_simplify == "SIMPLIFY AUTO 0.000001" &&
      data.redshift_templates.database.schema == null &&
      data.redshift_templates.database.id == "database-templates-listing-identity" &&
      output.copy_checks.database_templates == [for template in data.redshift_templates.database.templates : "${template.schema}.${template.name}"]
    )
    error_message = "Spatial COPY options and the unfiltered visible template listing must reach copy_checks."
  }

  assert {
    condition = (
      redshift_template.csv_events.database == redshift_database.local.name &&
      redshift_template.csv_events.schema == redshift_schema.local.name &&
      redshift_template.csv_events.format == "CSV" &&
      redshift_template.csv_events.parameters == tomap({ IGNOREHEADER = "1", TIMEFORMAT = "auto", EMPTYASNULL = "", ENCODING = "UTF8" })
    )
    error_message = "The CSV template must live in the workspace schema and hold its COPY parameters."
  }

  assert {
    condition = (
      redshift_template.json_events.owner == redshift_user.loader.name &&
      redshift_template.json_events.format == "JSON" &&
      redshift_template.json_events.format_argument == "auto ignorecase" &&
      redshift_template.json_events.parameters["MAXERROR"] == "10"
    )
    error_message = "The JSON template must be owned by the loader and carry its format argument."
  }

  assert {
    condition = (
      redshift_object_grant.loader_template.object_type == "TEMPLATE" &&
      redshift_object_grant.loader_template.object_name == redshift_template.csv_events.name &&
      redshift_object_grant.loader_template.schema_name == redshift_template.csv_events.schema &&
      redshift_object_grant.loader_template.grantee == redshift_user.loader.name &&
      redshift_object_grant.loader_template.privileges == toset(["USAGE"])
    )
    error_message = "The loader must hold USAGE on the CSV template."
  }

  assert {
    condition = (
      length(redshift_copy_job.fixture) == 0 && length(redshift_table.copy_target) == 0 &&
      length(data.redshift_copy_job.fixture) == 0 && output.copy_checks.copy_job_auto == null &&
      length(aws_redshift_integration.copy_job) == 0
    )
    error_message = "Without copy_job, no copy job, target table, or S3 event integration is created."
  }

  assert {
    condition = (
      output.copy_checks.template_owner == "observed_admin" &&
      output.copy_checks.template_format == "CSV" &&
      output.copy_checks.template_parameters["IGNOREHEADER"] == "1" &&
      output.copy_checks.schema_templates == [for template in data.redshift_templates.local.templates : template.name] &&
      output.copy_checks.copy_jobs == [for job in data.redshift_copy_jobs.local.copy_jobs : job.name] &&
      data.redshift_templates.local.schema == redshift_schema.local.name &&
      data.redshift_templates.local.id == "templates-listing-identity" &&
      data.redshift_copy_jobs.local.database == redshift_database.local.name
    )
    error_message = "The template lookups must be exposed through copy_checks."
  }
}

run "copy_job_defaults" {
  command = apply

  assert {
    condition = (
      length(redshift_copy_job.fixture) == 1 &&
      redshift_copy_job.fixture[0].source == "s3://${module.fixture_bucket.s3_bucket_id}/data/" &&
      redshift_copy_job.fixture[0].iam_role == aws_iam_role.consumer.arn &&
      redshift_copy_job.fixture[0].auto == false &&
      redshift_copy_job.fixture[0].format == "CSV" &&
      redshift_copy_job.fixture[0].parameters == tomap({ IGNOREHEADER = "1" })
    )
    error_message = "The copy job must load the fixture bucket's data prefix with the consumer role, without AUTO by default."
  }

  assert {
    condition = (
      aws_redshift_integration.copy_job[0].source_arn == module.fixture_bucket.s3_bucket_arn &&
      aws_redshift_integration.copy_job[0].target_arn == aws_redshiftserverless_namespace.consumer.arn &&
      contains(jsondecode(aws_redshift_resource_policy.inbound_integrations[0].policy).Statement[0].Condition.StringEquals["aws:SourceArn"], module.fixture_bucket.s3_bucket_arn) &&
      contains([for statement in jsondecode(local.fixture_bucket_policy).Statement : statement.Sid], "RedshiftEventIntegration")
    )
    error_message = "The copy job needs an S3 event integration from the fixture bucket that the bucket and the namespace authorize."
  }

  assert {
    condition = (
      redshift_copy_job.fixture[0].table.schema == redshift_schema.local.name &&
      redshift_copy_job.fixture[0].table.name == redshift_table.copy_target[0].name &&
      redshift_copy_job.fixture[0].table.columns == tolist(["id", "label"]) &&
      redshift_copy_job.fixture[0].database == redshift_database.local.name
    )
    error_message = "The copy job must target the managed copy table."
  }

  assert {
    condition = (
      output.copy_checks.copy_job_auto == false &&
      output.copy_checks.copy_job_role == "arn:aws:iam::111111111111:role/consumer-fixture" &&
      output.copy_checks.copy_job_source == "s3://mock-fixture/data/" &&
      output.copy_checks.copy_job_table.name == "example_copy_target" &&
      output.copy_checks.copy_job_format == "CSV" &&
      output.copy_checks.copy_job_parameters["IGNOREHEADER"] == "1"
    )
    error_message = "The copy job lookup must be exposed through copy_checks."
  }
}

run "copy_job_enable_auto" {
  command = apply
  variables { copy_job = { auto = true } }
  override_data {
    target = data.redshift_copy_job.fixture
    values = {
      auto            = true
      iam_role        = "arn:aws:iam::111111111111:role/consumer-fixture"
      source          = "s3://mock-fixture/data/"
      format          = "CSV"
      format_argument = null
      parameters      = { IGNOREHEADER = "1" }
      table           = { schema = "example_schema", name = "example_copy_target", columns = ["id", "label"] }
    }
  }
  assert {
    condition     = output.copy_checks.copy_job_auto == true
    error_message = "The same COPY JOB readback must verify and expose AUTO after an update."
  }
}
