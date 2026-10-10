# Mocked runs for views and materialized views. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

# Observed metadata deliberately differs from the configured resources, so the assertions show lookup wiring.
override_data {
  target = data.redshift_view.event_labels
  values = {
    id                     = "view-lookup-identity"
    owner                  = "observed_view_owner"
    late_binding           = false
    definition_fingerprint = "observed-view-fingerprint"
  }
}
override_data {
  target = data.redshift_materialized_view.label_counts
  values = {
    id                     = "materialized-view-lookup-identity"
    owner                  = "observed_mv_owner"
    auto_refresh           = true
    definition_fingerprint = "observed-mv-fingerprint"
  }
}

run "views_apply" {
  command = apply

  assert {
    condition = (
      redshift_view.event_labels.database == redshift_database.local.name &&
      redshift_view.event_labels.schema == redshift_schema.local.name &&
      redshift_view.event_labels.query == "SELECT id, label FROM public.${local.local_table_name}" &&
      redshift_view.event_labels_late.late_binding &&
      redshift_view.event_labels_late.schema == redshift_schema.local.name &&
      startswith(redshift_view.event_labels_late.query, "SELECT id, label FROM public.")
    )
    error_message = "Views must live in the managed consumer schema and query the schema-qualified events fixture."
  }
  assert {
    condition = (
      redshift_materialized_view.label_counts.database == redshift_database.local.name &&
      redshift_materialized_view.label_counts.schema == redshift_schema.local.name &&
      redshift_materialized_view.label_counts.distribution.style == "ALL" &&
      redshift_materialized_view.label_counts.distribution.key == null &&
      redshift_materialized_view.label_counts.sort_key.columns == tolist(["label"]) &&
      redshift_materialized_view.label_counts.auto_refresh &&
      strcontains(redshift_materialized_view.label_counts.query, "GROUP BY label")
    )
    error_message = "The materialized view must aggregate the events fixture with ALL distribution and automatic refresh."
  }
  assert {
    condition = (
      data.redshift_view.event_labels.database == redshift_view.event_labels.database &&
      data.redshift_view.event_labels.schema == redshift_view.event_labels.schema &&
      data.redshift_view.event_labels.name == redshift_view.event_labels.name &&
      data.redshift_materialized_view.label_counts.name == redshift_materialized_view.label_counts.name &&
      output.views == {
        event_labels = {
          id           = "view-lookup-identity"
          owner        = "observed_view_owner"
          late_binding = false
          fingerprint  = "observed-view-fingerprint"
        }
        label_counts = {
          id           = "materialized-view-lookup-identity"
          owner        = "observed_mv_owner"
          auto_refresh = true
          fingerprint  = "observed-mv-fingerprint"
        }
      }
    )
    error_message = "View lookups must select the managed views and expose their observations through output.views."
  }
}
