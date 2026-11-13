# Provider improvements

Open work on the provider. Implemented types and their ownership boundaries are listed in the
[provider documentation](docs/index.md#resources); development rules live in [DEVELOPMENT.md](DEVELOPMENT.md).

## Backlog

### Deferred

- **`redshift_system_setting`:** [ALTER SYSTEM](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_SYSTEM.html) has
  no readback of the persisted value and no reset. `redshift_configuration` reads the session value.
- **`redshift_connect_grant`:**
  [CONNECT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-connection-permissions) on a workgroup or
  cluster has no documented `REVOKE` form and no catalog readback.
- **`redshift_copy_job_grant`:** no catalog view or `SHOW` command reports `ALTER` or `DROP` on a COPY job.
- **`redshift_datashare_function`:** catalog rows omit overload signatures; same-name overloads are
  indistinguishable. Use exclusive schema-kind membership until overload-specific readback exists.
- **`redshift_debug_grant` on remote databases:** `SHOW GRANTS ON DATABASE "db@catalog"` can return no rows despite an
  existing ACCOUNT grant, and there is no reliable ACCOUNT-scoped alternative. Manage the grant through a producer-local
  provider.
- **`redshift_external_models`:** `SHOW MODEL ALL`, `SVV_ML_MODEL_INFO`, and `STV_ML_MODEL_INFO` report no model type,
  so only one `SHOW MODEL` per model tells external from trained models. Until a catalog reports the type,
  `redshift_models` lists all models as the listing of `redshift_external_model`.

### Not planned

- **`redshift_table_as`:** tables created by `CREATE TABLE AS` or `SELECT INTO` own data, not a definition.
- **`redshift_library`:** Python UDFs and their libraries reach end of support after June 30, 2026.
- **`redshift_compression_analysis`:** `ANALYZE COMPRESSION` takes an exclusive table lock, which a refresh must not
  do.
- **`redshift_query_plan`:** `EXPLAIN` is a diagnostic, not infrastructure state.
- **`redshift_external_grant`:** Lake Formation permissions overlap `aws_lakeformation_permissions`.
- **`redshift_external_view`:** Glue Data Catalog views belong to the Glue and Lake Formation side.
- **`redshift_model` training:** `CREATE MODEL` trains asynchronously in Amazon SageMaker, at cost, on every
  replacement.
- **`redshift_sql`:** no generic SQL resource. Transactions, cursors, prepared statements, `SET`/`RESET`/`USE`,
  queries and `CALL`, row mutation, one-time `COPY`/`UNLOAD`, and maintenance (`ANALYZE`, `VACUUM`,
  `REFRESH MATERIALIZED VIEW`, `CANCEL`, `LOCK`) are transport internals or pipeline actions.
- **`redshift_integration_refresh`:** `ALTER DATABASE ... INTEGRATION REFRESH` and per-table `HISTORY_MODE ... FOR TABLE`
  are zero-ETL actions, not state.

## Follow-ups

### Testing

- **Terraform nested-attribute mocks:** native `mock_provider` and `override_data` cannot populate nonempty computed
  nested lists/sets (verified with Terraform 1.17.2;
  [hashicorp/terraform#38369](https://github.com/hashicorp/terraform/issues/38369)). Complete-example readback
  postconditions therefore prevent ordinary mocked applies as well as negative runs from executing reliably.
  - **Temporary skip:** `Taskfile.yml`'s `terraform-check` prints an explicit skip instead of invoking
    `terraform -chdir=examples/complete test`. All suites and shared fixtures under `examples/complete/tests/` remain
    as restoration sources, not executed coverage. There is no custom fake-backend adapter or verification bypass.
  - **Retained checks and gap:** formatting, initialization, validation, provider Go contracts, and native mocks in
    `tests/acceptance/infra/tests/` still run. Normal local and release-example applies retain blocking AWS/catalog
    postconditions and behavioral SQL checks, but default deployments do not cover optional SSO, cross-account/model
    scenarios, negative fixture cases, or in-place update scenarios. Failures may surface only during live CI.
  - **Switch back:** first prove native mocks can populate nested lists/sets and nullable nested objects, and that both
    positive and negative postcondition runs pass on every supported Terraform version, updating retained fixtures as
    needed. Replace the skip in `Taskfile.yml` with `terraform -chdir=examples/complete test`, document the restored test
    command in `DEVELOPMENT.md`, and remove this limitation entry. Keep live postconditions intact.

### Features

- **`redshift_user_lockouts`:** Redshift 1.0.485645 rejects `SHOW USER LOCKOUT` ("account lockout security is not
  supported") on Serverless and provisioned; the complete example gates the listing behind `user_lockouts`. Recheck
  when it rolls out.
