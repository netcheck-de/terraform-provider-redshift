# Mocked runs for column and language grants and grant collections. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

# Observed catalog state, as the lookups would report it after apply.
override_data {
  target = data.redshift_column_grant.reader_events
  values = { privileges = { SELECT = ["id", "label"], UPDATE = ["label"] } }
}
override_data {
  target = data.redshift_language_grant.loader_sql
  values = { privileges = ["USAGE"], grant_option_privileges = ["USAGE"] }
}

# Terraform cannot override list-nested attributes of mocked data sources, so the listings stay empty here and the
# assertions check their selection and output wiring; Go tests cover the listed rows.

run "permissions_apply" {
  command = apply

  assert {
    condition = (
      redshift_column_grant.reader_events.database_name == redshift_database.local.name &&
      redshift_column_grant.reader_events.object_name == local.local_table_name &&
      redshift_column_grant.reader_events.grantee == redshift_role.readers.name && redshift_column_grant.reader_events.grantee_type == "ROLE" &&
      redshift_column_grant.reader_events.privileges["SELECT"] == toset(["id", "label"]) &&
      redshift_column_grant.reader_events.privileges["UPDATE"] == toset(["label"]) &&
      redshift_column_grant.group_event_labels.object_name == local.local_view_name &&
      redshift_column_grant.group_event_labels.grantee == redshift_group.readers.name &&
      length(redshift_column_grant.group_event_labels.privileges) == 1 && contains(keys(redshift_column_grant.group_event_labels.privileges), "SELECT")
    )
    error_message = "Column grants must cover the local table for the readers role and only SELECT on the view for the group."
  }

  assert {
    condition = (
      redshift_language_grant.operator_procedures.language_name == "plpgsql" &&
      redshift_language_grant.operator_procedures.grantee == redshift_role.operators.name &&
      redshift_language_grant.operator_procedures.privileges == toset(["USAGE"]) &&
      redshift_language_grant.loader_sql.language_name == "sql" && redshift_language_grant.loader_sql.grantee_type == "USER" &&
      redshift_language_grant.loader_sql.grantee == redshift_user.loader.name &&
      redshift_language_grant.loader_sql.grant_option_privileges == toset(["USAGE"])
    )
    error_message = "Language grants must give operators plpgsql USAGE and the loader sql USAGE with grant option."
  }

  assert {
    condition = (
      data.redshift_column_grant.reader_events.object_name == redshift_column_grant.reader_events.object_name &&
      data.redshift_language_grant.loader_sql.grantee == redshift_language_grant.loader_sql.grantee &&
      data.redshift_grants.local_events.object_type == "TABLE" && data.redshift_grants.local_events.object_name == local.local_table_name &&
      data.redshift_grants.loader.grantee == redshift_user.loader.name && data.redshift_grants.loader.object_type == null &&
      data.redshift_column_grants.local.schema_name == "public" && data.redshift_column_grants.local.object_name == null
    )
    error_message = "Lookups must select the managed tuples and the intended SHOW GRANTS and column listing forms."
  }

  assert {
    condition = (
      output.permission_checks.column_privileges["SELECT"] == toset(["id", "label"]) &&
      output.permission_checks.language_privileges == toset(["USAGE"]) &&
      output.permission_checks.language_grant_options == toset(["USAGE"]) &&
      length(output.permission_checks.table_grants) == length(data.redshift_grants.local_events.items) &&
      length(output.permission_checks.loader_grants) == length(distinct([for grant in data.redshift_grants.loader.items : grant.privilege])) &&
      length(output.permission_checks.column_grants) == length(data.redshift_column_grants.local.items)
    )
    error_message = "Permission outputs must expose the observed column, language, and listed grants."
  }
}
