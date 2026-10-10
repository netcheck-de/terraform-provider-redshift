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
