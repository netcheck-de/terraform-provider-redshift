# Mocked runs for functions and procedures. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

# Observed overloads report canonical signatures and catalog owners that differ from the configured spellings.
# Terraform mocks cannot override the elements of a nested attribute list, so the procedure lookup's argument output
# stays empty here; the provider's unit tests cover its contents.
override_data {
  target = data.redshift_function.label
  values = {
    id          = "function-lookup-identity"
    signature   = "INTEGER, CHARACTER VARYING"
    return_type = "CHARACTER VARYING"
    volatility  = "IMMUTABLE"
    owner       = "observed_function_owner"
  }
}
override_data {
  target = data.redshift_procedure.scale
  values = {
    id        = "procedure-lookup-identity"
    signature = "INTEGER, BIGINT"
    security  = "INVOKER"
    owner     = "observed_procedure_owner"
  }
}

run "routines_apply" {
  command = apply

  assert {
    condition = (
      redshift_function.label.database == redshift_schema.local.database &&
      redshift_function.label.schema == redshift_schema.local.name &&
      redshift_function.label.arguments == tolist(["INT", "VARCHAR(64)"]) &&
      redshift_function.label.return_type == "VARCHAR(128)" && redshift_function.label.volatility == "IMMUTABLE"
    )
    error_message = "The function must live in the consumer workspace schema with its configured overload."
  }
  assert {
    condition = (
      data.redshift_function.label.name == redshift_function.label.name &&
      data.redshift_function.label.arguments == redshift_function.label.arguments
    )
    error_message = "The function lookup must select the managed overload."
  }
  assert {
    condition = (
      redshift_object_grant.loader_label.object_type == "FUNCTION" &&
      redshift_object_grant.loader_label.schema_name == redshift_function.label.schema &&
      redshift_object_grant.loader_label.object_name == redshift_function.label.name &&
      redshift_object_grant.loader_label.arguments == "INT, VARCHAR(64)" &&
      redshift_object_grant.loader_label.grantee == redshift_user.loader.name &&
      redshift_object_grant.loader_label.privileges == toset(["EXECUTE"]) &&
      redshift_object_grant.loader_label.grant_option_privileges == toset(["EXECUTE"])
    )
    error_message = "The loader must hold EXECUTE with grant option on exactly the managed function overload."
  }
  assert {
    condition = (
      redshift_procedure.scale.schema == redshift_schema.local.name &&
      length(redshift_procedure.scale.argument) == 3 &&
      redshift_procedure.scale.argument[2].mode == "OUT" &&
      redshift_procedure.scale.configuration["search_path"] == redshift_schema.local.name &&
      strcontains(redshift_procedure.scale.body, "f_example_label(")
    )
    error_message = "The procedure must declare its IN, INOUT, and OUT arguments and resolve the function through search_path."
  }
  assert {
    condition = (
      data.redshift_procedure.scale.arguments == tolist(["INTEGER", "BIGINT"])
    )
    error_message = "The procedure lookup must select the overload by the IN and INOUT types of the argument blocks only."
  }
  assert {
    condition = (
      output.routines.function.signature == "INTEGER, CHARACTER VARYING" &&
      output.routines.function.owner == "observed_function_owner" &&
      output.routines.procedure.signature == "INTEGER, BIGINT" &&
      output.routines.procedure.security == "INVOKER" &&
      output.routines.procedure.id == "procedure-lookup-identity"
    )
    error_message = "The routines output must expose the observed lookups."
  }
}
