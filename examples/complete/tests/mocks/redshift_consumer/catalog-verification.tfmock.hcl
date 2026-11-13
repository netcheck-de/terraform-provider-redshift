# Targeted catalog observations for the complete example's blocking readback assertions.
override_data {
  target = data.redshift_database.collation_probe
  values = { collation = "CASE_SENSITIVE" }
}
override_data {
  target = data.redshift_database.local
  values = {
    database_type = "LOCAL", connection_limit = 100, collation = "CASE_SENSITIVE", isolation_level = "SNAPSHOT"
  }
}
override_data {
  target = data.redshift_database.shared
  values = {
    database_type    = "SHARED"
    datashare_arn    = "arn:aws:redshift:eu-central-1:111111111111:datashare:11111111-2222-3333-4444-555555555555/example_share"
    share_name       = "example_share"
    with_permissions = true
  }
}
override_data {
  target = data.redshift_schema.local
  values = { owner = "admin", quota = 2048 }
}
override_data {
  target = data.redshift_external_schema.shared
  values = {
    source_type = "REDSHIFT", source_database = "example_shared", source_schema = "public", owner = "example_loader"
  }
}
override_data {
  target = data.redshift_comment.local_schema
  values = { text = "Example consumer-local workspace." }
}
override_data {
  target = data.redshift_comments.local
  values = {
    id = "comments-listing-identity"
    comments = [
      { database_name = "example_local", object_type = "DATABASE", schema_name = null, object_name = "example_local", column_name = null, constraint_name = null, text = "Example consumer-local database." },
      { database_name = "example_local", object_type = "SCHEMA", schema_name = null, object_name = "example_schema", column_name = null, constraint_name = null, text = "Example consumer-local workspace." },
      { database_name = "example_local", object_type = "TABLE", schema_name = "public", object_name = "example_events", column_name = null, constraint_name = null, text = "Example events table created by bootstrap SQL." },
      { database_name = "example_local", object_type = "COLUMN", schema_name = "public", object_name = "example_events", column_name = "label", constraint_name = null, text = "Human-readable event label." },
      { database_name = "example_local", object_type = "VIEW", schema_name = "public", object_name = "example_event_labels", column_name = null, constraint_name = null, text = "Labels projected from the example events table." },
    ]
  }
}
override_data {
  target = data.redshift_table.accounts
  values = {
    column = [
      { name = "account_id", type = "INTEGER", nullable = false },
      { name = "name", type = "CHARACTER VARYING(128)", encoding = "ZSTD" },
    ]
    primary_key  = { columns = ["account_id"] }
    distribution = { style = "ALL", key = null }
  }
}
override_data {
  target = data.redshift_table.orders
  values = {
    id    = "orders-lookup-identity"
    owner = "example_loader"
    column = [
      { name = "order_id", type = "BIGINT", identity = { seed = 1, step = 1 } },
      { name = "account_id", type = "INTEGER", nullable = false, encoding = "AZ64" },
      { name = "status", type = "CHARACTER VARYING(16)", encoding = "BYTEDICT" },
      { name = "amount", type = "NUMERIC(12,2)", encoding = "AZ64" },
      { name = "created_at", type = "TIMESTAMP WITHOUT TIME ZONE", nullable = false },
    ]
    primary_key            = { columns = ["order_id"] }
    unique                 = [{ columns = ["account_id", "created_at"] }]
    foreign_key            = [{ columns = ["account_id"], references = { schema = "example_schema", table = "example_accounts", columns = ["account_id"] } }]
    distribution           = { style = "KEY", key = "account_id" }
    sort_key               = { style = "COMPOUND", columns = ["created_at", "order_id"] }
    effective_distribution = { style = "KEY", key = "account_id", auto = false }
  }
}
override_data {
  target = data.redshift_table.events
  values = {
    column = [
      { name = "event_id", type = "BIGINT", renamed_from = null },
      { name = "payload", type = "SUPER", renamed_from = null },
    ]
    effective_distribution = { style = "EVEN", key = null, auto = true }
    effective_sort_key     = { style = "NONE", columns = [], auto = true }
  }
}
override_data {
  target = data.redshift_comment.orders_key
  values = { text = "Surrogate key of an example order." }
}

# Fingerprints represent independently read canonical catalog text, never raw configured SQL.
override_resource {
  target = redshift_view.event_labels
  values = { late_binding = false, definition_fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }
}
override_resource {
  target = redshift_view.event_labels_late
  values = { definition_fingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }
}
override_resource {
  target = redshift_view.order_totals
  values = { definition_fingerprint = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" }
}
override_resource {
  target = redshift_materialized_view.label_counts
  values = { definition_fingerprint = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" }
}
override_data {
  target = data.redshift_view.event_labels
  values = {
    id                     = "view-lookup-identity", owner = "observed_view_owner", late_binding = false
    definition_fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}
override_data {
  target = data.redshift_materialized_view.label_counts
  values = {
    id                     = "materialized-view-lookup-identity", owner = "observed_mv_owner", auto_refresh = true
    definition_fingerprint = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
    distribution           = null
    sort_key               = null
  }
}
override_data {
  target = data.redshift_views.example
  values = {
    id = "views-listing-identity"
    views = [
      { database = "example_local", schema = "example_schema", name = "event_labels", late_binding = false, definition_fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" },
      { database = "example_local", schema = "example_schema", name = "event_labels_late", late_binding = true, definition_fingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
      { database = "example_local", schema = "example_schema", name = "order_totals", late_binding = true, definition_fingerprint = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" },
    ]
  }
}
override_resource {
  target = redshift_function.label
  values = { definition_fingerprint = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" }
}
override_resource {
  target = redshift_procedure.scale
  values = { definition_fingerprint = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" }
}
override_data {
  target = data.redshift_function.label
  values = {
    id                     = "function-lookup-identity", owner = "observed_function_owner", signature = "INTEGER, CHARACTER VARYING"
    return_type            = "CHARACTER VARYING", volatility = "IMMUTABLE", language = "SQL"
    definition_fingerprint = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
  }
}
override_data {
  target = data.redshift_procedure.scale
  values = {
    id                     = "procedure-lookup-identity", owner = "observed_procedure_owner", signature = "INTEGER, BIGINT", security = "INVOKER"
    definition_fingerprint = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
    argument = [
      { name = "factor", type = "INTEGER", mode = null },
      { name = "amount", type = "BIGINT", mode = "INOUT" },
      { name = "label", type = "CHARACTER VARYING", mode = "OUT" },
    ]
  }
}
override_data {
  target = data.redshift_object_grant.loader_label
  values = { privileges = ["EXECUTE"], grant_option_privileges = ["EXECUTE"] }
}
override_data {
  target = data.redshift_databases.example
  values = { databases = [{ name = "example_local", database_type = "LOCAL" }] }
}
override_data {
  target = data.redshift_schemas.local
  values = { schemas = [{ database = "example_local", name = "example_schema", quota = 2048 }] }
}
override_data {
  target = data.redshift_constraints.discovery_keys
  values = {
    constraints = [{ database = "example_local", schema = "public", table = "example_discovery_keys", name = "example_discovery_keys_pkey", constraint_type = "PRIMARY KEY", columns = ["id"] }]
  }
}
override_data {
  target = data.redshift_comment.discovery_key
  values = { text = "Example primary key found by redshift_constraints." }
}
