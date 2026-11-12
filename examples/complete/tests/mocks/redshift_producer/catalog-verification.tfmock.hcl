# Address-specific observations avoid changing unrelated data sources of the same type.
override_data {
  target = data.redshift_database.producer
  values = { database_type = "LOCAL", owner = "admin", collation = "CASE_SENSITIVE", isolation_level = "SNAPSHOT", connection_limit = -1 }
}
override_data {
  target = data.redshift_datashare.producer
  values = {
    publicly_accessible = false, owner = "observed_share_owner", share_id = 100
    producer_namespace  = "11111111-2222-3333-4444-555555555555"
  }
}
override_data {
  target = data.redshift_datashare.grants
  values = { publicly_accessible = false, share_id = 101 }
}
override_data {
  target = data.redshift_datashare_schema.source
  values = { exists = true, include_new = true }
}
override_data {
  target = data.redshift_datashare_table.source
  values = { exists = true }
}
override_data {
  target = data.redshift_datashare_schemas.producer
  values = {
    id = "producer-schemas-identity"
    datashare_schemas = [
      { database = "example_producer", datashare = "example_share", schema = "public", include_new = true },
      { database = "example_producer", datashare = "example_share", schema = "example_bulk_share", include_new = false },
    ]
  }
}
override_data {
  target = data.redshift_datashare_membership.bulk["TABLES"]
  values = { exists = true, all = true }
}
override_data {
  target = data.redshift_datashare_membership.bulk["FUNCTIONS"]
  values = { exists = true, all = true }
}
override_data {
  target = data.redshift_datashare_grant.consumer
  values = { exists = true }
}
override_data {
  target = data.redshift_datashare_grant.lake_formation[0]
  values = { exists = true }
}
override_data {
  target = data.redshift_grant.share_schema
  values = { privileges = ["SELECT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.share_schema_usage
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_role.share_operators
  values = { role_id = 100, owner = "admin" }
}
override_data {
  target = data.redshift_datashare_privilege.share_operators
  values = { privileges = ["ALTER", "SHARE"] }
}
override_data {
  target = data.redshift_schemas.producer
  values = { schemas = [{ database = "example_producer", name = "example_bulk_share" }] }
}
override_data {
  target = data.redshift_tables.bulk_share
  values = { tables = [{ database = "example_producer", schema = "example_bulk_share", name = "events" }] }
}
override_data {
  target = data.redshift_functions.bulk_share
  values = {
    functions = [
      { database = "example_producer", schema = "example_bulk_share", name = "f_shared_identity", signature = "INTEGER", return_type = "INTEGER", volatility = "IMMUTABLE", language = "SQL" },
      { database = "example_producer", schema = "example_bulk_share", name = "f_shared_identity", signature = "CHARACTER VARYING", return_type = "CHARACTER VARYING", volatility = "IMMUTABLE", language = "SQL" },
    ]
  }
}
override_data {
  target = data.redshift_tables.fixture
  values = { tables = [{ database = "example_producer", schema = "public", name = "fixture" }] }
}
override_data {
  target = data.redshift_columns.fixture
  values = {
    columns = [
      { database = "example_producer", schema = "public", table = "fixture", name = "id", ordinal_position = 1, data_type = "INTEGER" },
      { database = "example_producer", schema = "public", table = "fixture", name = "label", ordinal_position = 2, data_type = "CHARACTER VARYING" },
    ]
  }
}
