output "views" {
  description = "Observed view and materialized view metadata from the read-only lookups and listings of the example schema."
  value = {
    event_labels = {
      id           = data.redshift_view.event_labels.id
      owner        = data.redshift_view.event_labels.owner
      late_binding = data.redshift_view.event_labels.late_binding
      fingerprint  = data.redshift_view.event_labels.definition_fingerprint
    }
    label_counts = {
      id           = data.redshift_materialized_view.label_counts.id
      owner        = data.redshift_materialized_view.label_counts.owner
      auto_refresh = data.redshift_materialized_view.label_counts.auto_refresh
      fingerprint  = data.redshift_materialized_view.label_counts.definition_fingerprint
    }
    views              = { for view in data.redshift_views.example.views : view.name => view.late_binding }
    materialized_views = { for view in data.redshift_materialized_views.example.materialized_views : view.name => view.auto_refresh }
  }
}
