# Provider improvements

Open work on the provider. Implemented types and their ownership boundaries are listed in the
[provider documentation](docs/index.md#resources); development rules live in [DEVELOPMENT.md](DEVELOPMENT.md).

## Backlog

### Deferred

These wait for a documented way to read their state back:

- **`redshift_system_setting`:** [ALTER SYSTEM](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_SYSTEM.html) has
  no readback of the persisted value and no reset. `redshift_configuration` reads the session value.
- **`redshift_connect_grant`:**
  [CONNECT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-connection-permissions) on a workgroup or
  cluster has no documented `REVOKE` form and no catalog readback.
- **COPY job grants:** no catalog view or `SHOW` command reports `ALTER` or `DROP` on a COPY job.
- **Individual datashare function membership:** catalog rows omit overload signatures; same-name overloads are
  indistinguishable. Use exclusive schema-kind membership until overload-specific readback exists.
- **Remote ACCOUNT DEBUG grants:** `SHOW GRANTS ON DATABASE "db@catalog"` can return no rows despite an existing
  grant, and there is no reliable ACCOUNT-scoped alternative. Manage the grant through a producer-local provider.

### Not planned

- **`redshift_table_as`:** tables created by `CREATE TABLE AS` or `SELECT INTO` own data, not a definition.
- **`redshift_library`:** Python UDFs and their libraries reach end of support after June 30, 2026.
- **Compression analysis:** `ANALYZE COMPRESSION` takes an exclusive table lock, which a refresh must not do.
- **`redshift_query_plan`:** `EXPLAIN` is a diagnostic, not infrastructure state.
- **`redshift_external_grant`:** Lake Formation permissions overlap `aws_lakeformation_permissions`.
- **`redshift_external_view`:** Glue Data Catalog views belong to the Glue and Lake Formation side.
- **`redshift_model` training:** `CREATE MODEL` trains asynchronously in Amazon SageMaker, at cost, on every
  replacement.
- **Execution and session commands:** transactions, cursors, prepared statements, `SET`/`RESET`/`USE`, queries and
  `CALL`, row mutation, one-time `COPY`/`UNLOAD`, and maintenance (`ANALYZE`, `VACUUM`, `REFRESH MATERIALIZED VIEW`,
  `CANCEL`, `LOCK`) are transport internals or pipeline actions. No generic SQL resource.
- **Zero-ETL actions:** `ALTER DATABASE ... INTEGRATION REFRESH` and per-table `HISTORY_MODE ... FOR TABLE`.

## Follow-ups

### Features

- **User lockouts:** Redshift 1.0.485645 rejects `SHOW USER LOCKOUT` ("account lockout security is not supported") on
  Serverless and provisioned; the complete example gates the listing behind `user_lockouts`. Recheck when it rolls out.

### Live checks

- **Concurrency hardening:** confirm against Redshift that a cancelled Data API statement reaches a final state within
  the settle wait, and that repeated `GRANT ... ON ALL` and `ADD ALL` rounds cover objects created in parallel.
