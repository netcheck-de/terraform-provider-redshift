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

- **`redshift_table` column renames:** a `renamed_from` argument on `column` blocks, so a rename runs
  `ALTER TABLE ... RENAME COLUMN` instead of dropping and re-adding the column, which loses its data.
- **`restore_default_on_destroy`:** destroying a `PUBLIC` `redshift_language_grant` or the database-wide `PUBLIC`
  `FUNCTIONS` tuple of `redshift_default_privileges` keeps the revocation; an opt-in argument could grant the default
  back instead of the documented `removed` block workaround.
- **Shared-database object grants:** grants on objects of a database created from a datashare need their own identity
  and catalog reads in `redshift_object_grant`.
- **Datashare bulk membership and functions:** `ADD ALL TABLES IN SCHEMA` and `ADD FUNCTION` lack a membership
  resource; define their ownership against `redshift_datashare_table` and `include_new`.
- **Database collation in place:** `ALTER DATABASE ... COLLATE` is restricted, so a `collation` change replaces.
- **`redshift_function.arguments` null elements:** add `listvalidator.NoNullValues()` to the resource and its lookup;
  `routineStrings` drops nulls, so a null element selects a shorter overload.
- **Plan-time validation:** most resources skip `ValidateConfig` unless the whole configuration is known
  (`column_grant`, `database`, `external_table`, `grant`, …); do as `redshift_materialized_view` does where checks
  skip unknown values.
- **External model import:** settings that `SHOW MODEL` does not report stay null after import, so the first plan
  replaces the model. Record "unread after import" in private state and skip that replacement until the first apply.
- **COPY job definition drift:** `table`, `source`, `format`, `format_argument`, and `parameters` are never read back;
  if `COPY JOB SHOW` prints a parseable definition, parse it with `sqlclient.Tokens` and `parseCopyOptions`.
- **COPY templates:** `SHAPEFILE SIMPLIFY` is not modeled. The listing without a schema runs one `SHOW TEMPLATES` per
  schema and fails on a schema the identity cannot use; `SYS_REDSHIFT_TEMPLATE` might replace the loop.
- **Zero-ETL destinations:** `DROP DATABASE ... FORCE` if replication sessions block a plain drop; a DynamoDB source in
  the complete example.
- **Session context variables:** `redshift_configuration` rejects dotted names such as `app_context.user_id`.

### Documentation wording

- **Lookup descriptions:** `data.redshift_external_table` reuses the resource's `table_properties` description; override
  it in `lookupDescriptions`.
- **Fingerprint descriptions:** lookups and listings of functions, procedures, RLS and masking policies describe
  `definition_fingerprint` with resource wording; override it as the view lookups do (`describedOutputs` covers listing
  elements).
- **External table update error:** the guard's error text does not mention the switch to positional mapping; rewording
  it changes eight golden files.

## Live verification (`TF_ACC=1`)

Catalog behaviors the offline tests cannot reach. Each bullet names the fix if the assumption is wrong.

### Tables, views, and functions

- `TestAccTableLifecycle`: interior column drops and their `PlanOnly` no-drift steps.
- `ADD COLUMN ... NOT NULL` without a default is refused (why the provider replaces the table).
- Dropping an `AUTO`-chosen distribution or sort key column: the `EVEN`/`NONE` detour and the return to `AUTO`;
  `ALTER DISTSTYLE` fails under an interleaved sort key.
- `PG_CLASS_INFO` and `SVV_REDSHIFT_COLUMNS.distkey` are visible to a non-superuser (`effective_distribution`).
- `ALTER DISTSTYLE`/`DISTKEY`/`SORTKEY` on materialized views, including removing the blocks.
- The stored procedure `function_type` in `SVV_REDSHIFT_FUNCTIONS` (matched as `%PROCEDURE%`).
- The implicit `PUBLIC` `USAGE` on `sql` and `plpgsql` in `SVV_LANGUAGE_PRIVILEGES`.
- The `<table>_pkey` name of an unnamed primary key (used in a complete example comment).
- Spectrum maps Parquet columns by name. If so, extend `externalTableMapsByName` to Parquet.

### COPY templates and jobs

- How `SHOW TEMPLATE` prints each keyword in `copy_parameters.go` (`FORMAT AS`, `NULL AS`, `ACCEPTINVCHARS`, `ON`/`OFF`
  values, integers, quoting, escapes), and whether every keyword is accepted in a template.
- `SHOW TEMPLATES FROM SCHEMA` columns, `LIKE`, and the cross-database `"db"."schema"` form the `TEMPLATE` grant check
  uses.
- `CREATE`/`ALTER TEMPLATE` as rendered; whether `CREATE OR REPLACE` keeps owner and grants; names fold to lowercase.
- `SHOW GRANTS ON TEMPLATE`: `privilege_scope = TEMPLATE`, identity types, and grant options.
- `COPY JOB CREATE`/`ALTER`/`DROP` as rendered through the Data API, including `AUTO OFF` without an S3 event
  integration, and the scope of job name uniqueness.
- `SYS_COPY_JOB`: non-superuser visibility, padding, `iam_role` text, `is_auto` values, and whether rows are per
  database. If warehouse-wide, filter the read and `redshift_copy_jobs` by database, e.g. via `COPY JOB LIST`.

### External models and model grants

- `SHOW MODEL ALL` column labels and whether it lists only the current database.
- `SHOW MODEL` keys for an external Bedrock model (model ID, prompt, suffix, request/response type, function, role,
  owner). Adjust `observeExternalModel`, read exactly those keys, and refuse non-Bedrock models.
- `CREATE EXTERNAL MODEL` as rendered, and whether it fails without Bedrock model access; `DROP MODEL` drops the
  function outside a transaction.
- `GRANT`/`REVOKE EXECUTE ON MODEL` for every grantee kind and grant option, and whether `PUBLIC` gets `EXECUTE`
  implicitly.
- `SVV_ML_MODEL_INFO` lists external models of other databases for the provider identity. If not, add a hook in
  `privilege_resource.go` that checks with `SHOW MODEL ALL` in the target database; until then a `MODEL` grant on an
  external model leaves state on every refresh.
- `SVV_ML_MODEL_PRIVILEGES` reports `EXECUTE` rows for external models.
- The complete example: `bedrock:InvokeModel` on the foundation-model ARN suffices on Redshift Serverless.

### Zero-ETL destinations

- The `database_type` `SHOW DATABASES` reports for a destination (the fake assumes `integration`). Then add it to the
  `data.redshift_databases` filter, report the integration there, and let the lookup skip local databases.
- `CREATE DATABASE ... FROM INTEGRATION ... DATABASE '<source>'` with a string literal, including mixed case.
- `SVV_INTEGRATION` from the administration database: zero rows (not an error) for a non-superuser,
  `auto_remediation`, `source_database` for MySQL and DynamoDB, and `integration_id` matching the RDS ARN.
- `PG_DATABASE_INFO` and the isolation level for a destination; `OWNER TO`, `CONNECTION LIMIT`, `ISOLATION LEVEL` on it.
- Each `INTEGRATION SET` option as an autocommit statement, visible at once; create as soon as the integration is
  active; plain `DROP DATABASE`.
- The complete example: case-sensitive identifiers on the consumer and the namespace resource policy.

### Federated grants and diagnostics

- `SHOW GRANTS ON DATABASE` reports `DEBUG` rows (`global_user`, `global_group`, `admins_of_account` with the bare
  account ID), including for `"db@catalog"`; `GRANT`/`REVOKE DEBUG` works from the administration database.
- Whether `SHOW DATABASES` lists `db@catalog` names; otherwise a transient failure removes the grant from state.
- The complete example with `federated_debug_database`: which warehouse needs federated permissions.
- `SHOW <parameter>` column naming, `ALTER USER ... SET` defaults, and `ALTER SYSTEM` values through all transports.
- `SHOW USER LOCKOUT` columns, value formats, and the non-superuser failure.

## Fixtures

Supported connection methods are in the [connection guide](docs/guides/connection_methods.md). Remaining live
validation needs:

- **Provisioned clusters:** lifecycle, import, drift, and transport-switch tests with Data API temporary-user and direct
  IAM authentication, plus Secrets Manager credentials for Data API secret mode.
- **Cross-account sharing:** the [complete example](examples/complete/README.md) against disposable producer and
  consumer warehouses, including a non-superuser reader and unauthorized-write rejection.
- **Identity Center:** integration replacement and deletion of Terraform-owned roles with a dedicated managed
  application; federated users that block integration deletion.
- **ASSUMEROLE:** a warehouse with identity-specific access control already enabled, so the lifecycle suite does not
  revoke `PUBLIC` IAM-role access on a shared warehouse.

Use isolated state and uniquely named objects. Direct tests need endpoint connectivity and matching certificate
hostnames; offline tests never need AWS credentials.
