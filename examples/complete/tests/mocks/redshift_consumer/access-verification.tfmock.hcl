# Positive catalog observations shared by every complete-example suite. Keep overrides address-specific:
# type-wide defaults would give unrelated grants the wrong privilege set or grant options.
override_data {
  target = data.redshift_user.reader
  values = {
    superuser        = false
    create_database  = false
    valid_until      = null
    connection_limit = 5
    session_timeout  = 3600
    syslog_access    = "RESTRICTED"
    search_path      = ["$user", "public"]
    session_defaults = { timezone = "UTC", statement_timeout = "300000" }
  }
}
override_data {
  target = data.redshift_user.iam_auditor
  values = { superuser = false, create_database = false, valid_until = null, connection_limit = 2, syslog_access = "UNRESTRICTED" }
}
override_data {
  target = data.redshift_user.loader
  values = { superuser = false, create_database = true, valid_until = null }
}
override_data {
  target = data.redshift_group.readers
  values = { group_id = 104, members = ["example_reader"] }
}
override_data {
  target = data.redshift_group_membership.reader
  values = { exists = true }
}
override_data {
  target = data.redshift_role.readers
  values = { role_id = 101, owner = "tfadmin" }
}
override_data {
  target = data.redshift_role.operators
  values = { role_id = 102, owner = "tfadmin" }
}
override_data {
  target = data.redshift_role.auditors
  values = { role_id = 103, owner = "tfadmin" }
}
override_data {
  target = data.redshift_role_grant.reader
  values = { exists = true, admin_option = false }
}
override_data {
  target = data.redshift_role_grant.operators
  values = { exists = true, admin_option = false }
}
override_data {
  target = data.redshift_role_grant.loader_operators
  values = { exists = true, admin_option = true }
}

override_data {
  target = data.redshift_grant.shared_read["DATABASE"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.shared_read["SCHEMAS"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.shared_read["TABLES"]
  values = { privileges = ["SELECT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.local_schema
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.loader_schema_tables
  values = { privileges = ["SELECT", "INSERT"], grant_option_privileges = ["SELECT"] }
}
override_data {
  target = data.redshift_grant.remaining["schema_tables"]
  values = { privileges = ["SELECT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.remaining["schema_functions"]
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.remaining["schema_procedures"]
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.remaining["languages"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.remaining["copy_jobs"]
  values = { privileges = ["CREATE", "ALTER", "DROP"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_grant.remaining["templates"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}

override_data {
  target = data.redshift_object_grant.group_schema
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.operators_public_tables
  values = { privileges = ["SELECT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.shared_reader["DATABASE"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.shared_reader["SCHEMA"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.shared_reader["TABLE"]
  values = { privileges = ["SELECT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.shared_function
  values = { privileges = ["EXECUTE"], grant_option_privileges = ["EXECUTE"] }
}
override_data {
  target = data.redshift_object_grant.shared_snapshots["ALL TABLES"]
  values = { privileges = ["SELECT"], grant_option_privileges = ["SELECT"] }
}
override_data {
  target = data.redshift_object_grant.shared_snapshots["ALL FUNCTIONS"]
  values = { privileges = ["EXECUTE"], grant_option_privileges = ["EXECUTE"] }
}
override_data {
  target = data.redshift_object_grant.shared_procedure
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.shared_procedure_snapshot
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["loader_events"]
  values = { privileges = ["SELECT", "INSERT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["loader_schema"]
  values = { privileges = ["CREATE", "USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["operators_database"]
  values = { privileges = ["TEMPORARY"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["public_schema"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["reader_events_option"]
  values = { privileges = ["SELECT"], grant_option_privileges = ["SELECT"] }
}
override_data {
  target = data.redshift_object_grant.remaining["shared_bulk_schema"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["snapshot_DATABASE"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["snapshot_SCHEMA"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["procedure_DATABASE"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.remaining["procedure_SCHEMA"]
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}

override_data {
  target = data.redshift_system_grant.operators
  values = { privileges = ["CREATE ROLE"] }
}
override_data {
  target = data.redshift_assumerole_grant.reader
  values = { privileges = ["COPY", "UNLOAD"] }
}
override_data {
  target = data.redshift_assumerole_grant.loader[0]
  values = { privileges = ["COPY"] }
}
override_data {
  target = data.redshift_assumerole_grant.operators_any_role[0]
  values = { privileges = ["UNLOAD", "RESULT_OFFLOAD"] }
}
override_data {
  target = data.redshift_default_privilege.reader_tables
  values = { privileges = ["SELECT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_default_privilege.loader_functions_public
  values = { privileges = [], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_default_privilege.remaining["schema_tables"]
  values = { privileges = ["SELECT"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_default_privilege.remaining["reader_option"]
  values = { privileges = ["SELECT"], grant_option_privileges = ["SELECT"] }
}
override_data {
  target = data.redshift_default_privilege.remaining["FUNCTIONS"]
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_default_privilege.remaining["PROCEDURES"]
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}

override_data {
  target = data.redshift_language_grant.public_sql
  values = { privileges = [], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_language_grant.loader_sql
  values = { privileges = ["USAGE"], grant_option_privileges = ["USAGE"] }
}
override_data {
  target = data.redshift_language_grant.operator_procedures
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_column_grant.reader_events
  values = { privileges = { SELECT = ["id", "label"], UPDATE = ["label"] } }
}
override_data {
  target = data.redshift_column_grant.groups["event_labels"]
  values = { privileges = { SELECT = ["label"] } }
}
override_data {
  target = data.redshift_column_grant.groups["orders"]
  values = { privileges = { SELECT = ["order_id", "account_id", "status", "created_at"] } }
}
