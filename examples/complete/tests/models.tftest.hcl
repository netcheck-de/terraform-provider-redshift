# Mocked runs for machine learning models. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

run "models_default_off" {
  command = apply

  assert {
    condition = (
      length(redshift_external_model.summarizer) == 0 &&
      length(aws_iam_role_policy.consumer_bedrock) == 0 &&
      length(redshift_object_grant.readers_summarizer) == 0 &&
      length(redshift_user.model_delegate) == 0 &&
      length(redshift_object_grant.model_delegate_schema) == 0 &&
      length(redshift_object_grant.delegate_summarizer) == 0 &&
      length(redshift_object_grant.group_summarizer) == 0 &&
      length(redshift_object_grant.public_summarizer) == 0 &&
      length(data.redshift_user.model_delegate) == 0 &&
      length(data.redshift_object_grant.model_delegate_schema) == 0 &&
      length(data.redshift_object_grant.delegate_summarizer) == 0 &&
      length(data.redshift_object_grant.group_summarizer) == 0 &&
      length(data.redshift_object_grant.public_summarizer) == 0 &&
      length(data.redshift_external_model.summarizer) == 0
    )
    error_message = "Without bedrock_model_id, no Bedrock permission, model, or model grant may be created."
  }

  assert {
    condition = (
      data.redshift_models.local.database == redshift_schema.local.database &&
      data.redshift_models.local.schema == redshift_schema.local.name &&
      output.model_checks.summarizer == null
    )
    error_message = "The model listing must cover the consumer-local schema, and the Bedrock checks must stay null."
  }

  # Terraform mocks cannot populate nested list attributes, so the listing stays empty here; the Go tests cover its
  # contents, and this checks that the output is derived from the listing alone.
  assert {
    condition     = output.model_checks.schema_models == []
    error_message = "The schema model check must expose only what the model listing observes."
  }
}

run "models_bedrock" {
  command = apply

  variables {
    bedrock_model_id = "amazon.nova-lite-v1:0"
  }

  override_data {
    target = data.redshift_external_model.summarizer[0]
    values = {
      id       = "external-model-lookup-identity"
      function = "f_example_summarize"
      model_id = "amazon.nova-lite-v1:0"
      owner    = "observed_model_owner"
    }
  }

  override_data {
    target = data.redshift_object_grant.readers_summarizer[0]
    values = { privileges = ["EXECUTE"] }
  }

  override_data {
    target = data.redshift_object_grant.model_delegate_schema[0]
    values = { privileges = ["USAGE"] }
  }

  override_data {
    target = data.redshift_object_grant.delegate_summarizer[0]
    values = {
      privileges              = ["EXECUTE"]
      grant_option_privileges = ["EXECUTE"]
    }
  }

  override_data {
    target = data.redshift_object_grant.group_summarizer[0]
    values = { privileges = ["EXECUTE"] }
  }

  override_data {
    target = data.redshift_object_grant.public_summarizer[0]
    values = { privileges = [] }
  }

  assert {
    condition = (
      redshift_external_model.summarizer[0].database == redshift_schema.local.database &&
      redshift_external_model.summarizer[0].schema == redshift_schema.local.name &&
      redshift_external_model.summarizer[0].iam_role == "DEFAULT" &&
      redshift_external_model.summarizer[0].model_id == var.bedrock_model_id &&
      redshift_external_model.summarizer[0].request_type == "UNIFIED" &&
      redshift_external_model.summarizer[0].response_type == "VARCHAR"
    )
    error_message = "The external model must live in the consumer-local schema and call the configured Bedrock model through the default role."
  }

  assert {
    condition = (
      aws_iam_role_policy.consumer_bedrock[0].role == aws_iam_role.consumer.id &&
      jsondecode(aws_iam_role_policy.consumer_bedrock[0].policy).Statement[0].Action == "bedrock:InvokeModel" &&
      endswith(jsondecode(aws_iam_role_policy.consumer_bedrock[0].policy).Statement[0].Resource, "::foundation-model/${var.bedrock_model_id}")
    )
    error_message = "Only the consumer warehouse role may invoke exactly the configured Bedrock model."
  }

  assert {
    condition = (
      redshift_object_grant.readers_summarizer[0].object_type == "MODEL" &&
      redshift_object_grant.readers_summarizer[0].schema_name == redshift_external_model.summarizer[0].schema &&
      redshift_object_grant.readers_summarizer[0].object_name == redshift_external_model.summarizer[0].name &&
      redshift_object_grant.readers_summarizer[0].grantee == redshift_role.readers.name &&
      redshift_object_grant.readers_summarizer[0].privileges == toset(["EXECUTE"])
    )
    error_message = "Readers must receive EXECUTE on the model, which covers its inference function."
  }

  assert {
    condition = (
      output.model_checks.summarizer.function == "${redshift_schema.local.name}.f_example_summarize" &&
      output.model_checks.summarizer.model_id == var.bedrock_model_id &&
      output.model_checks.summarizer.owner == "observed_model_owner" &&
      output.model_checks.summarizer.reader_privileges == toset(["EXECUTE"]) &&
      output.model_checks.summarizer.delegate_name == redshift_user.model_delegate[0].name &&
      output.model_checks.summarizer.delegate_schema_privileges == toset(["USAGE"]) &&
      output.model_checks.summarizer.user_privileges == toset(["EXECUTE"]) &&
      output.model_checks.summarizer.user_grant_option_privileges == toset(["EXECUTE"]) &&
      output.model_checks.summarizer.group_privileges == toset(["EXECUTE"]) &&
      length(output.model_checks.summarizer.public_privileges) == 0
    )
    error_message = "The model checks must expose what the model and grant lookups observe."
  }

  assert {
    condition = (
      redshift_user.model_delegate[0].password_disabled &&
      redshift_object_grant.model_delegate_schema[0].object_type == "SCHEMA" &&
      redshift_object_grant.model_delegate_schema[0].grantee == redshift_user.model_delegate[0].name &&
      redshift_object_grant.model_delegate_schema[0].privileges == toset(["USAGE"]) &&
      redshift_object_grant.delegate_summarizer[0].object_type == "MODEL" &&
      redshift_object_grant.delegate_summarizer[0].object_name == redshift_external_model.summarizer[0].name &&
      redshift_object_grant.delegate_summarizer[0].grantee_type == "USER" &&
      redshift_object_grant.delegate_summarizer[0].grantee == redshift_user.model_delegate[0].name &&
      redshift_object_grant.delegate_summarizer[0].privileges == toset(["EXECUTE"]) &&
      redshift_object_grant.delegate_summarizer[0].grant_option_privileges == toset(["EXECUTE"])
    )
    error_message = "Only the separate IAM-only model delegate receives schema USAGE and user EXECUTE WITH GRANT OPTION."
  }

  assert {
    condition = (
      redshift_object_grant.group_summarizer[0].object_type == "MODEL" &&
      redshift_object_grant.group_summarizer[0].object_name == redshift_external_model.summarizer[0].name &&
      redshift_object_grant.group_summarizer[0].grantee_type == "GROUP" &&
      redshift_object_grant.group_summarizer[0].grantee == redshift_group.readers.name &&
      redshift_object_grant.group_summarizer[0].privileges == toset(["EXECUTE"]) &&
      length(redshift_object_grant.group_summarizer[0].grant_option_privileges) == 0 &&
      redshift_object_grant.public_summarizer[0].object_type == "MODEL" &&
      redshift_object_grant.public_summarizer[0].object_name == redshift_external_model.summarizer[0].name &&
      redshift_object_grant.public_summarizer[0].grantee_type == "PUBLIC" &&
      redshift_object_grant.public_summarizer[0].grantee == "public" &&
      length(redshift_object_grant.public_summarizer[0].privileges) == 0 &&
      length(redshift_object_grant.public_summarizer[0].grant_option_privileges) == 0
    )
    error_message = "The group receives only EXECUTE, while the PUBLIC tuple remains explicitly empty without widening access."
  }
}
