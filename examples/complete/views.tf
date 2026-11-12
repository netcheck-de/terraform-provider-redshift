# Views over the consumer-local events fixture. They live in the managed example schema and depend on it, so Terraform
# drops them before the schema's restrictive DROP SCHEMA.
resource "redshift_view" "event_labels" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "event_labels"
  query    = "SELECT id, label FROM public.${local.local_table_name}"

  depends_on = [aws_redshiftdata_statement.local_table]
}

data "redshift_view" "event_labels" {
  provider = redshift.consumer
  database = redshift_view.event_labels.database
  schema   = redshift_view.event_labels.schema
  name     = redshift_view.event_labels.name

  lifecycle {
    postcondition {
      # Compare catalog fingerprints, not configured SQL: Redshift qualifies names and rewrites expressions.
      condition = (
        self.late_binding == redshift_view.event_labels.late_binding &&
        length(self.definition_fingerprint) == 64 && self.definition_fingerprint == redshift_view.event_labels.definition_fingerprint
      )
      error_message = "The bound event-label view must retain its binding mode and catalog definition."
    }
  }
}

# A late-binding view is not bound to the table, so the table can be dropped or altered independently.
resource "redshift_view" "event_labels_late" {
  provider     = redshift.consumer
  database     = redshift_schema.local.database
  schema       = redshift_schema.local.name
  name         = "event_labels_late"
  late_binding = true
  query        = "SELECT id, label FROM public.${local.local_table_name}"
}

resource "redshift_materialized_view" "label_counts" {
  provider     = redshift.consumer
  database     = redshift_schema.local.database
  schema       = redshift_schema.local.name
  name         = "label_counts"
  auto_refresh = true
  query        = "SELECT label, COUNT(*) AS events FROM public.${local.local_table_name} GROUP BY label"

  distribution {
    style = "ALL"
  }

  sort_key {
    columns = ["label"]
  }

  depends_on = [aws_redshiftdata_statement.local_table]
}

data "redshift_materialized_view" "label_counts" {
  provider = redshift.consumer
  database = redshift_materialized_view.label_counts.database
  schema   = redshift_materialized_view.label_counts.schema
  name     = redshift_materialized_view.label_counts.name

  lifecycle {
    postcondition {
      # Storage options are deliberately not checked: this lookup cannot observe distribution/sort keys.
      condition = (
        self.auto_refresh == redshift_materialized_view.label_counts.auto_refresh &&
        length(self.definition_fingerprint) == 64 && self.definition_fingerprint == redshift_materialized_view.label_counts.definition_fingerprint
      )
      error_message = "The label-count materialized view must retain its canonical catalog definition and configured automatic-refresh flag."
    }
  }
}

# A view over a Terraform-managed table. Late binding keeps the table's own changes possible: Redshift refuses DROP
# COLUMN and DROP TABLE on a table that an ordinary view depends on, and the provider never cascades. Referencing the
# table still creates the view after it and drops the view first.
resource "redshift_view" "order_totals" {
  provider     = redshift.consumer
  database     = redshift_table.orders.database
  schema       = redshift_table.orders.schema
  name         = "order_totals"
  late_binding = true
  query        = "SELECT account_id, COUNT(*) AS orders, SUM(amount) AS amount FROM ${redshift_table.orders.schema}.${redshift_table.orders.name} GROUP BY account_id"
}

# Listings of the managed example schema: ordinary and late-binding views, and materialized views.
data "redshift_views" "example" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name

  depends_on = [redshift_view.event_labels, redshift_view.event_labels_late, redshift_view.order_totals]

  lifecycle {
    postcondition {
      condition = alltrue([
        for expected in [redshift_view.event_labels, redshift_view.event_labels_late, redshift_view.order_totals] :
        anytrue([for observed in self.views :
          observed.name == expected.name && observed.schema == expected.schema &&
          observed.late_binding == expected.late_binding && length(observed.definition_fingerprint) == 64 &&
          observed.definition_fingerprint == expected.definition_fingerprint
        ])
      ])
      error_message = "The view catalog must contain all three managed views with their bound/late-binding flags and canonical definitions."
    }
  }
}

data "redshift_materialized_views" "example" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name

  depends_on = [redshift_materialized_view.label_counts]
}
