# Mocked runs for COPY templates, the template grant, and the optional copy job. The mocks are shared with
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

# Observed catalog values deliberately differ from the configuration where Redshift reports its own spelling.
override_data {
  target = data.redshift_template.csv_events
  values = {
    owner      = "observed_admin"
    format     = "CSV"
    parameters = { IGNOREHEADER = "1", TIMEFORMAT = "auto", EMPTYASNULL = "", ENCODING = "UTF8" }
  }
}
override_data {
  target = data.redshift_templates.local
  values = { id = "templates-listing-identity" }
}
override_data {
  target = data.redshift_copy_jobs.local
  values = { id = "copy-jobs-listing-identity" }
}

run "copy_templates" {
  command = apply

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

run "copy_job_enabled" {
  command = apply

  variables {
    copy_job = {}
  }

  override_data {
    target = data.redshift_copy_job.fixture
    values = { auto = false, iam_role = "arn:aws:iam::222222222222:role/observed" }
  }

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
      output.copy_checks.copy_job_role == "arn:aws:iam::222222222222:role/observed"
    )
    error_message = "The copy job lookup must be exposed through copy_checks."
  }
}
