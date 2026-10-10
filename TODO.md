# Provider improvements

This file inventories potential SQL resource/data-source implementations, follow-ups to implemented types, and
outstanding integration tests against the AWS
[SQL commands index](https://docs.aws.amazon.com/redshift/latest/dg/c_SQL_commands.html). The implementation reference
and development rules live in
[DEVELOPMENT.md](DEVELOPMENT.md#sql-implementation-reference).

## SQL resource and data-source implementation backlog

The command families below cover durable objects, permissions, settings, and metadata discovery in the index that the
provider does not implement yet. `CREATE`, `ALTER`, and `DROP` are lifecycle operations of an object resource, not
separate resource types. Each new object or relationship should have a corresponding read-only data source where
catalog discovery is available. Candidate type names are provisional; they are not registered provider types. Existing
coverage and the ownership boundaries between types are listed in the [provider documentation](docs/index.md#resources).

### Improvements to existing resources

| Existing resource   | AWS SQL command references                                                                                                                                                       | Outstanding implementation scope                                                                                                                                           |
|---------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `redshift_database` | [CREATE DATABASE](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_DATABASE.html), [ALTER DATABASE](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DATABASE.html) | Zero-ETL integration-backed databases and their settings, and other catalog-backed database forms. Keep one-time integration refresh outside configuration reconciliation. |

### New resources and data sources

#### Object, relationship, and setting implementations

Each candidate below includes its corresponding single-object or single-relationship data source. List/discovery data
sources are inventoried separately. Definitions need import identities, catalog readback, drift handling, documented
mutable/replacement attributes, and dependency-aware deletion before implementation is considered complete.

| Candidate resource and paired data source                 | AWS SQL command references                                                                                                                                                                                                                                                                           | Outstanding implementation scope                                                                                                                                                                                                                   |
|-----------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Query-created tables: evaluate `redshift_table_as`        | [CREATE TABLE AS](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_TABLE_AS.html), [SELECT INTO](https://docs.aws.amazon.com/redshift/latest/dg/r_SELECT_INTO.html), [DROP TABLE](https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_TABLE.html)                                           | Persistent query-created tables need an explicit contract for derived schema, initialization, replacement, and ownership of subsequently modified data. Temporary tables are session objects.                                                      |
| `redshift_external_view`                                  | [CREATE EXTERNAL VIEW](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_EXTERNAL_VIEW.html), [ALTER EXTERNAL VIEW](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_EXTERNAL_VIEW.html), [DROP EXTERNAL VIEW](https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_EXTERNAL_VIEW.html) | External catalog view definitions, dialect/catalog metadata, ownership, and documented update/replacement semantics.                                                                                                                               |
| `redshift_model`                                          | [CREATE MODEL](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_MODEL.html), [SHOW MODEL](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_MODEL.html), [DROP MODEL](https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_MODEL.html)                                                   | Redshift ML trained/BYOM forms, generated inference function ownership, settings, asynchronous status/readback, and deletion of associated artifacts.                                                                                              |
| `redshift_external_model`                                 | [CREATE EXTERNAL MODEL](https://docs.aws.amazon.com/redshift/latest/dg/r_create_external_model.html), [SHOW MODEL](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_MODEL.html), [DROP MODEL](https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_MODEL.html)                                 | Bedrock model/inference-function bindings, IAM role, prompt/suffix, request/response settings, and readback. Avoid overlapping ownership of the generated function.                                                                                |
| `redshift_template`                                       | [CREATE TEMPLATE](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_TEMPLATE.html), [ALTER TEMPLATE](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TEMPLATE.html), [DROP TEMPLATE](https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_TEMPLATE.html)                               | Reusable COPY formatting/conversion configuration, ownership, catalog definition, and create-or-replace semantics.                                                                                                                                 |
| `redshift_copy_job`                                       | [COPY](https://docs.aws.amazon.com/redshift/latest/dg/r_COPY.html) → [COPY JOB](https://docs.aws.amazon.com/redshift/latest/dg/r_COPY-JOB.html)                                                                                                                                                      | Persistent job definition, S3/table/IAM bindings, AUTO configuration, SHOW/LIST lookup, and DROP lifecycle. JOB RUN and actual row ingestion are execution actions.                                                                                |
| `redshift_system_setting`                                 | [ALTER SYSTEM](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_SYSTEM.html), [SHOW](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW.html)                                                                                                                                               | Persistent SQL-level settings such as `metadata_security`, `data_catalog_auto_mount`, and `default_identity_namespace`; verify authoritative readback, activation timing, and reset/delete semantics. Avoid AWS parameter-group ownership overlap. |
| Legacy library lookup/import: evaluate `redshift_library` | [CREATE LIBRARY](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_LIBRARY.html), [DROP LIBRARY](https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_LIBRARY.html), [CREATE FUNCTION](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_FUNCTION.html)                                 | The index still lists Python-library commands, but AWS documents Python UDF end of support after June 30, 2026. Treat these as legacy discovery/migration candidates, not a backlog for new `plpythonu` deployment.                                |

#### New permission implementations

These permission families currently lack a corresponding exact-tuple implementation. Each candidate includes its paired
read-only lookup. A family may instead become a new supported object/recipient form of an existing resource if that
preserves a clear, non-overlapping identity.

| Candidate resource and paired data source             | AWS SQL command references                                                                                                                                                                                        | Outstanding implementation scope                                                                                                                                |
|-------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `redshift_model_grant`                                | [GRANT model permissions](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-model-syntax), [REVOKE](https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html)                                 | EXECUTE on a specific model and CREATE MODEL recipient forms not covered by role system privileges.                                                             |
| `redshift_template_grant` / `redshift_copy_job_grant` | [GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html), [REVOKE](https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html)                                                                      | Template USAGE/ALTER/DROP and COPY-job ALTER/DROP, with catalog reads and exact object identities.                                                              |
| `redshift_external_grant`                             | [GRANT Lake Formation permissions](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-spectrum-integration-with-lf-syntax), [REVOKE](https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html) | IAM-role and supported PUBLIC permissions on external schemas/tables, column lists, and grant options. Avoid overlapping AWS-provider Lake Formation ownership. |
| `redshift_connect_grant`                              | [GRANT connection permissions](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-connection-permissions), [REVOKE](https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html)                  | Workgroup/cluster CONNECT for Identity Center federated users, roles, and PUBLIC where federated permissions are enabled.                                       |
| `redshift_debug_grant`                                | [GRANT DEBUG](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-debug-syntax), [REVOKE](https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html)                                             | Database DEBUG granted to global identities, Identity Center groups, or producer-account administrators.                                                        |

#### Additional catalog and diagnostic data sources

Single-object lookups pair with the resources above. Collection and metadata lookups below expose observations without
owning or changing the objects. `SHOW`/`DESC` are candidate discovery interfaces, not requirements to replace a working
catalog reader with a different SQL command.

| Candidate data source or extension                                    | AWS SQL command references                                                                                                                                                   | Outstanding implementation scope                                                                                                                                                           |
|-----------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `redshift_external_view`                                              | [SHOW VIEW](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_VIEW.html)                                                                                                 | External catalog view definitions; catalog reads must cover kinds not exposed by SHOW VIEW.                                                                                                |
| `redshift_model` / `redshift_external_model`                          | [SHOW MODEL](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_MODEL.html)                                                                                               | Model definition, inference binding, training/status metadata, and supported external model fields.                                                                                        |
| `redshift_templates` / `redshift_template`                            | [SHOW TEMPLATES](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_TEMPLATES.html), [SHOW TEMPLATE](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_TEMPLATE.html) | COPY-template discovery and stored configuration.                                                                                                                                          |
| `redshift_copy_jobs` / `redshift_copy_job`                            | [COPY JOB LIST/SHOW](https://docs.aws.amazon.com/redshift/latest/dg/r_COPY-JOB.html)                                                                                         | Persistent job configuration and status without starting a load.                                                                                                                           |
| `redshift_configuration` / `redshift_system_setting`                  | [SHOW](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW.html)                                                                                                           | Effective configuration/context-variable observation; explicitly identify session values versus persistent system settings.                                                                |
| `redshift_user_lockout`                                               | [SHOW USER LOCKOUT](https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_USER_LOCKOUT.html)                                                                                 | Password-user lockout state and documented counters/settings; reading must not unlock a user.                                                                                              |
| `redshift_query_plan`                                                 | [EXPLAIN](https://docs.aws.amazon.com/redshift/latest/dg/r_EXPLAIN.html)                                                                                                     | Read-only query-plan diagnostics, including policy explanations when authorized; no query execution or object ownership.                                                                   |
| Compression recommendations: evaluate `redshift_compression_analysis` | [ANALYZE COMPRESSION](https://docs.aws.amazon.com/redshift/latest/dg/r_ANALYZE_COMPRESSION.html)                                                                             | Advisory encoding output does not change encodings, but acquires an exclusive table lock. Evaluate an opt-in diagnostic interface rather than executing it during routine catalog refresh. |

### Execution and session commands

These remaining command families do not define an ordinary durable resource lifecycle. Use them as internal transport
operations or operator/pipeline actions, except for the persistent configuration and diagnostic candidates explicitly
identified above. A generic SQL execution resource/data source must not masquerade as declarative object ownership.

| Command family                           | AWS SQL command references                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | Implementation treatment                                                                                                                                                                                            |
|------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Transactions                             | [ABORT](https://docs.aws.amazon.com/redshift/latest/dg/r_ABORT.html), [BEGIN](https://docs.aws.amazon.com/redshift/latest/dg/r_BEGIN.html), [START TRANSACTION](https://docs.aws.amazon.com/redshift/latest/dg/r_START_TRANSACTION.html), [COMMIT](https://docs.aws.amazon.com/redshift/latest/dg/r_COMMIT.html), [END](https://docs.aws.amazon.com/redshift/latest/dg/r_END.html), [ROLLBACK](https://docs.aws.amazon.com/redshift/latest/dg/r_ROLLBACK.html)                                                                                                                      | Internal transaction handling; no standalone resource or catalog data source.                                                                                                                                       |
| Cursors                                  | [DECLARE](https://docs.aws.amazon.com/redshift/latest/dg/declare.html), [FETCH](https://docs.aws.amazon.com/redshift/latest/dg/fetch.html), [CLOSE](https://docs.aws.amazon.com/redshift/latest/dg/close.html)                                                                                                                                                                                                                                                                                                                                                                      | Session-bound transport/query behavior, not persisted Terraform objects.                                                                                                                                            |
| Prepared statements                      | [PREPARE](https://docs.aws.amazon.com/redshift/latest/dg/r_PREPARE.html), [EXECUTE](https://docs.aws.amazon.com/redshift/latest/dg/r_EXECUTE.html), [DEALLOCATE](https://docs.aws.amazon.com/redshift/latest/dg/r_DEALLOCATE.html)                                                                                                                                                                                                                                                                                                                                                  | Session-bound execution, not durable function/procedure definitions.                                                                                                                                                |
| Session state                            | [SET](https://docs.aws.amazon.com/redshift/latest/dg/r_SET.html), [RESET](https://docs.aws.amazon.com/redshift/latest/dg/r_RESET.html), [SET SESSION AUTHORIZATION](https://docs.aws.amazon.com/redshift/latest/dg/r_SET_SESSION_AUTHORIZATION.html), [SET SESSION CHARACTERISTICS](https://docs.aws.amazon.com/redshift/latest/dg/r_SET_SESSION_CHARACTERISTICS.html), [USE](https://docs.aws.amazon.com/redshift/latest/dg/r_USE_command.html)                                                                                                                                    | Connection/session behavior. Persistent user/system defaults belong to the setting candidates above; SHOW is observational.                                                                                         |
| Query execution and data materialization | [SELECT](https://docs.aws.amazon.com/redshift/latest/dg/r_SELECT_synopsis.html), [SELECT INTO](https://docs.aws.amazon.com/redshift/latest/dg/r_SELECT_INTO.html), [CREATE TABLE AS](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_TABLE_AS.html), [CALL](https://docs.aws.amazon.com/redshift/latest/dg/r_CALL_procedure.html), [EXPLAIN](https://docs.aws.amazon.com/redshift/latest/dg/r_EXPLAIN.html)                                                                                                                                                                 | Pipelines execute queries/procedures. Query-created persistent table definitions and read-only explain diagnostics need the separate contracts above; do not track arbitrary query results as owned infrastructure. |
| Row mutation                             | [INSERT](https://docs.aws.amazon.com/redshift/latest/dg/r_INSERT_30.html), [INSERT (external table)](https://docs.aws.amazon.com/redshift/latest/dg/r_INSERT_external_table.html), [UPDATE](https://docs.aws.amazon.com/redshift/latest/dg/r_UPDATE.html), [DELETE](https://docs.aws.amazon.com/redshift/latest/dg/r_DELETE.html), [MERGE](https://docs.aws.amazon.com/redshift/latest/dg/r_MERGE.html), [TRUNCATE](https://docs.aws.amazon.com/redshift/latest/dg/r_TRUNCATE.html), [ALTER TABLE APPEND](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TABLE_APPEND.html) | Data transformations/loading and destructive row operations are pipeline/operator actions, not table-definition reconciliation.                                                                                     |
| Loading/export                           | [COPY](https://docs.aws.amazon.com/redshift/latest/dg/r_COPY.html), [UNLOAD](https://docs.aws.amazon.com/redshift/latest/dg/r_UNLOAD.html)                                                                                                                                                                                                                                                                                                                                                                                                                                          | One-time loads and exports remain execution actions; persistent COPY JOB definitions are a separate resource candidate.                                                                                             |
| Maintenance and query control            | [ANALYZE](https://docs.aws.amazon.com/redshift/latest/dg/r_ANALYZE.html), [ANALYZE COMPRESSION](https://docs.aws.amazon.com/redshift/latest/dg/r_ANALYZE_COMPRESSION.html), [VACUUM](https://docs.aws.amazon.com/redshift/latest/dg/r_VACUUM_command.html), [REFRESH MATERIALIZED VIEW](https://docs.aws.amazon.com/redshift/latest/dg/materialized-view-refresh-sql-command.html), [CANCEL](https://docs.aws.amazon.com/redshift/latest/dg/r_CANCEL.html), [LOCK](https://docs.aws.amazon.com/redshift/latest/dg/r_LOCK.html)                                                      | Runtime maintenance, diagnostics, refresh, or control actions. Automatic-refresh configuration belongs to the materialized-view resource; compression diagnostics require the explicit evaluation above.            |

## Follow-ups to implemented types

### Features

- **`redshift_table` column renames:** a `renamed_from` argument on `column` blocks, so a rename runs
  `ALTER TABLE ... RENAME COLUMN` instead of dropping the column and adding a new one, which loses its data.
- **`redshift_external_table` Parquet name mapping:** Parquet columns are reconciled by position like text and CSV. Once
  a live check confirms that Spectrum maps Parquet columns by name, extend `externalTableMapsByName` to Parquet so
  adds and drops anywhere and reorders change the table in place.
- **`restore_default_on_destroy`:** destroying a `PUBLIC` `redshift_language_grant` or the database-wide `PUBLIC`
  `FUNCTIONS` tuple of `redshift_default_privileges` keeps the revocation. An opt-in argument could grant the Redshift
  default back on destroy instead of the documented `removed` block workaround.
- **Shared-database object grants:** `redshift_object_grant` covers local objects only; grants on objects of a database
  created from a datashare (`database.schema.object`) need their own identity and catalog reads.
- **Datashare bulk membership and functions:** `ALTER DATASHARE ... ADD ALL TABLES IN SCHEMA` and `ADD FUNCTION` lack a
  membership resource; define their ownership against `redshift_datashare_table` and the `include_new` schema policy.
- **Database collation in place:** `ALTER DATABASE ... COLLATE` exists but is restricted, so a `collation` change
  replaces the database. Support it in place once the restrictions are documented precisely enough to plan around.
- **`redshift_function.arguments` null elements:** add `listvalidator.NoNullValues()` to the resource and its lookup.
  `routineStrings` drops null elements, so a null element currently creates or selects a shorter overload.
- **Plan-time validation:** `redshift_materialized_view` runs its `ValidateConfig` checks on partially unknown
  configurations. Other resources still return early unless the whole configuration is known (`column_grant`,
  `database`, `external_table`, `grant`, and others); those whose checks already skip unknown values can do the same.

### Documentation wording

- **Lookup descriptions:** `data.redshift_external_table` reuses the resource's `table_properties` description, which
  explains plans and replacement. Override it in `lookupDescriptions`, as the column descriptions already are.
- **Fingerprint descriptions:** the lookups and listings of functions, procedures, RLS policies, and masking policies
  describe `definition_fingerprint` with the resource wording "detects definition changes made outside Terraform".
  Override it as `redshift_view` and `redshift_materialized_view` already do; `describedOutputs` also covers the
  elements of a listing.
- **External table update error:** the guard's error text ("column blocks change in place only by adding or dropping
  columns, at the end unless an ORC table maps columns by name...") does not name the switch to positional mapping.
  Rewording it changes eight golden files, so it was kept for now.

### Live verification (`TF_ACC=1`)

The offline tests cannot reach these catalog behaviors; confirm them against a real workgroup or cluster:

- `TestAccTableLifecycle`: the interior column drop steps and their `PlanOnly` no-drift checks.
- Redshift refuses `ADD COLUMN ... NOT NULL` without a default, which is why the provider replaces the table instead.
- Dropping a column that Redshift chose as an `AUTO` distribution or sort key: the `ALTER DISTSTYLE EVEN` /
  `ALTER SORTKEY NONE` detour, the drop, and the return to `AUTO`.
- `ALTER DISTSTYLE` fails on a table with an interleaved sort key, which the replacement for dropping an `AUTO`
  distribution key under `INTERLEAVED` relies on.
- `PG_CLASS_INFO` and `SVV_REDSHIFT_COLUMNS.distkey` visibility for a non-superuser provider identity, which
  `effective_distribution` depends on.
- The `function_type` spelling of stored procedures in `SVV_REDSHIFT_FUNCTIONS`; the provider matches `%PROCEDURE%`
  because AWS documents only `REGULAR FUNCTION`.
- Whether the implicit `PUBLIC` `USAGE` on `sql` and `plpgsql` appears in `SVV_LANGUAGE_PRIVILEGES`.
- `ALTER DISTSTYLE`, `ALTER DISTKEY`, and `ALTER SORTKEY` on materialized views, including removing the blocks.
- Whether Spectrum maps Parquet columns by name (see the Parquet follow-up above).
- The `<table>_pkey` name Redshift gives an unnamed primary key, which the complete example's constraint comment uses.

### Test reliability

- One `task check` run exited with status 201 right after a background `go test` was killed, and two clean reruns
  passed. Watch for a flaky race test and investigate if it happens again.

## Outstanding integration tests

Supported methods are documented in the [connection guide](docs/guides/connection_methods.md): Serverless/provisioned
Data API, direct password, and direct IAM for both warehouse types. Remaining live validation needs dedicated fixtures:

- **Provisioned clusters:** run lifecycle, import, drift, and transport-switch tests using both Data API temporary-user
  and direct IAM authentication. Add Secrets Manager credentials for Data API secret-mode validation.
- **Cross-account sharing:** run the [complete example](examples/complete/README.md) against disposable producer and
  consumer warehouses, including a non-superuser shared-data reader session and unauthorized-write rejection.
- **Identity Center:** validate integration replacement and deletion of Terraform-owned roles with a dedicated managed
  application; explicitly handle federated users that block integration deletion.
- **ASSUMEROLE:** provide a test warehouse with identity-specific access control already enabled. The normal lifecycle
  suite must not revoke unrestricted PUBLIC IAM-role access on a shared warehouse.

Use isolated Terraform state and uniquely named SQL objects. Direct tests need endpoint connectivity and certificate
hostnames matching the discovered/custom domain; offline tests never require AWS credentials.
