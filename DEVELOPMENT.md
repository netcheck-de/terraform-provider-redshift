# Development and releases

The `netcheck-de/redshift` provider manages Redshift SQL objects using terraform-plugin-framework, protocol 6,
through the Data API or direct TLS. Use the AWS
[Redshift SQL reference](https://docs.aws.amazon.com/redshift/latest/dg/c_SQL_commands.html) for SQL behavior.

## Repository layout

```text
.
|-- docs/               # Generated documentation
|-- examples/
|   |-- complete/       # Integrated usage
|   |   `-- tests/      # Composition suites and fixtures
|   |       `-- mocks/  # Shared provider mocks
|   |-- data-sources/   # Data-source examples
|   |-- provider/       # Provider configuration examples
|   `-- resources/      # Resource and import examples
|-- internal/
|   |-- provider/       # Resources, data sources, and provider wiring
|   |-- redshiftconn/   # Direct pgx transport
|   |-- redshiftdata/   # Data API transport
|   `-- sqlclient/      # Client interface, SQL builders, mutation serialization
|-- tests/
|   `-- acceptance/     # Live acceptance fixtures and CI/recovery guide
|       |-- infra/      # Independent live fixtures using the pinned released provider
|           `-- tests/  # Offline mocked fixture tests
`-- templates/          # Documentation templates
```

## Local checks

Install Go matching `go.mod`, Terraform 1.16+, Task v3, `curl`, yamllint, and markdownlint-cli
(`brew install yamllint markdownlint-cli`). Run commands from the repository root.

| Command                          | Purpose                                                  |
|----------------------------------|----------------------------------------------------------|
| `task check`                     | Lint, formatting, offline race tests, coverage, and docs |
| `task test`                      | Offline tests with race detection and coverage           |
| `task fmt` / `task markdown-fmt` | Format Go / Markdown                                     |
| `task lint` / `task lint-fix`    | Go lint / autofix                                        |
| `task golden`                    | Regenerate SQL goldens                                   |
| `task docs` / `task docs-check`  | Generate / validate documentation                        |
| `task terraform-check`           | Terraform formatting, validation, and offline examples   |
| `task terraform -- <args>`       | Local provider CLI; `TF_DIR` selects the configuration   |

Builds and caches live in `.cache/`. Task installs the golangci-lint version in `.golangci-lint-version`; other pinned
tools are `tool` directives in `go.mod`, invoked with `go tool <name>`.

CI tests Terraform 1.16 and the latest release. Terraform 1.11–1.15 are untested because their `terraform test` passes
unset optional ephemeral inputs to apply and rejects the run
([hashicorp/terraform#38974](https://github.com/hashicorp/terraform/issues/38974), fixed in 1.16.0).

Run `task check` before submitting changes; also run `task terraform-check` when Terraform configurations change.

## Architecture and lifecycle

- Resources access Redshift through `sqlclient.Client`; transport and AWS service clients belong in provider wiring.
- Validate tuples before the first `State.Set` in Create, and expose the same check in `ValidateConfig`.
- Create/Update re-read the catalog to verify convergence. Read calls `RemoveResource` when the object or its parent
  is gone; genuine query failures remain errors.
- Synchronous DROP needs no post-delete refresh. Use only checks needed for safe deletion; retain convergence checks
  for permission reconciliation and asynchronous operations.

## Required test coverage

Add or update tests alongside behavior changes; extend existing suites rather than duplicating their coverage.

| Change                   | Required coverage                                                                     |
|--------------------------|---------------------------------------------------------------------------------------|
| SQL rendering/validation | Unit tests and SQL goldens where applicable                                           |
| Resource behavior        | Offline Terraform lifecycle/contracts and live acceptance coverage                    |
| Data source              | Offline read/filter/error tests and live assertions against concrete fixtures         |
| New resource/data source | Complete-example configuration, outputs, and mocked composition tests, plus the above |
| Bug fix                  | Regression test reproducing the failure                                               |
| Optional feature         | Explicit acceptance prerequisites and example CI flags where fixtures are available   |

- Resource acceptance tests should cover create, meaningful updates, import, no-change plans, and deletion where supported.
  Add drift and failure-recovery cases when relevant to the behavior being changed.
- Data-source acceptance tests must check actual returned fields from known fixtures, not just successful reads or empty
  results. Include filters and missing-object behavior where supported.
- Mocked example tests check composition, not live SQL behavior; they do not replace acceptance tests.
- Keep complete-example and fixture composition validation in Terraform, not the provider's Go test package.
- A skipped acceptance test is not executed coverage. Keep fixture requirements explicit and enable self-contained
  optional example features in CI; do not assume externally managed identities or account settings exist.
- Adding acceptance coverage is required; running AWS tests locally is not. Report which checks ran and which tests
  skipped or still need live CI fixtures. Never deploy resources without authorization.

## Go test conventions

- `go test ./...` runs offline unit tests and Terraform CLI contracts; Terraform is required, AWS credentials are not.
  Contracts use `*_contract_test.go`, `TestContract...`, and `resource.UnitTest`. Fake SQL with `queryFunc` or `catalog`.
- Live tests use `*_acceptance_test.go`, `TestAcc...`, and `resource.Test`, gated by `testAccPreCheck` /
  `testAccWorkgroup` and `TF_ACC=1`.
- Use testify: `require` for preconditions/errors, `assert` for independent checks. Use `testing/synctest` for in-process
  timeout/retry/concurrency tests; it does not control Terraform subprocesses or socket I/O.
- Test provider behavior, not framework internals or invalid synthetic schemas. Check built-in plan modifiers through
  schema wiring; test custom planning and partial-failure recovery behavior.
  Successful `resource.UnitTest` applies already check empty plans with and without refresh; avoid redundant plan steps.
- Assert state and mutations for ordinary lifecycles; use transcripts only for significant mutation ordering, not routine
  refresh/import queries. Test recovery from returned partial-create state separately from SQL error injection.

## Adding resources and data sources

Use the registration hooks rather than adding per-type branches to shared infrastructure. Keep `TODO.md` limited to
open work.

1. Register in the type's own file: `var _ = registerResource(newX)` or `registerDataSource(newX)`.
2. Put pure renderers in `<type>_sql.go`, using `alterStep` for alterations. Reuse `privilegeResource` / `grantSpec` for
   permission resources where applicable. Pin SQL with `checkSQL`; use `runTranscripts` for ordering-sensitive flows.
3. Register `registerReplacementPolicy`, `registerParity` (or an exemption), `registerLifecycleCase`, and the catalog
   fake's `registerFakeFamily` in the type's tests. Cover every configurable input in the replacement policy and meet
   the [required test coverage](#required-test-coverage) for the new type.
4. Add `templates/<kind>/<name>.md.tmpl` and per-type examples. Resource examples include `resource.tf`, `import.tf`, and
   `import.sh`; data sources use `data-source.tf`. Follow the [documentation format](#documentation).
5. Extend `examples/complete/<block>.tf`, `outputs_<block>.tf`, and `tests/<block>.tftest.hcl`, using shared mocks.
   Update the type tables and ownership matrix in `templates/index.md.tmpl`, then run `task check` and
   `task terraform-check`.

### Schema conventions

- Nested configuration uses singular blocks (`column`, `distribution`); lists of plain values use plural attributes.
  Required blocks use the matching `IsRequired()` validator, plus `SizeAtLeast(1)` for lists/sets. Their descriptions
  start with "At least one … is required." Absent list/set blocks are null; modifiers must preserve collection shape.
- Data sources never declare blocks. `newCatalogDataSource` / `collectionElement` expose computed nested attributes;
  lookup-only inputs use `catalogSpec.selectors`, and listing results use the data-source name. Parity registration
  covers shared fields/selectors; unpaired lookups use `standalone`, resources without lookups need exemptions.
- Use `foldNames` at the end of `Schema` (`folded` for privilege contracts) for names Redshift folds to lowercase;
  `TestEveryIdentifierRejectsUppercase` lists exemptions.
- Attribute descriptions describe values; Reconciliation tables explain replacement. Use `lookupDescriptions` when
  resource wording does not fit a lookup.
- Schema validators check local constraints; shared domain checks validate relationships as their inputs become known.
- Long-running operations use `operationTimeoutsBlock`, `timeouts.Value`, and `boundOperation`; exclude timeouts from
  lookups and replacement/alter comparisons.
- IDs are provider-relative JSON tuples containing only object-identifying fields.

### SQL and golden files

- Build SQL with `sqlclient.Stmt` / `Fragment` and catalog queries with `Select(...).Build()`, never concatenation.
  Unquoted `Keyword` values must be constants, validated constructors (`OneOf`, `TypeName`, `ColumnType`, `Signature`),
  or reviewed `//sql:trusted` conversions. Configured SQL uses `CheckUserSQL` and `UserSQL` via `Verbatim`.
- SQL types and keywords use uppercase; preserve equivalent configured spelling and never rewrite user-supplied SQL.
  Catalog privilege names may differ from configuration names; see `normalizePrivilege`.

Golden files are checked-in expectations under `internal/provider/testdata/sql/<group>/<case>.sql`. Normal tests compare
current SQL output against them, catching unintended changes and making intentional changes reviewable as diffs:

- Renderer goldens (`checkSQL`) record generated statements and catalog queries.
- Transcript goldens (`runTranscripts`) record significant SQL sequences, particularly mutation ordering.

When SQL output intentionally changes:

1. Run the tests and inspect the differences.
2. Run `task golden` to regenerate the expectations from the current implementation.
3. Review every changed golden against the AWS SQL reference, then commit it with the code.

Regeneration accepts the current output; it does not prove the SQL is correct or validate it against a live Redshift
warehouse. The task needs no warehouse connection. It also removes stale files within each group, so each group must
have exactly one owning renderer or transcript test.

## Terraform example tests

### Run the complete example with a local provider

From the repository root, the `terraform` task builds the provider and selects its isolated CLI configuration:

```sh
task terraform -- init -backend=false
task terraform -- apply
task terraform -- output -json warehouses
task terraform -- output -json verification
```

Unlike offline tests, these commands deploy real AWS resources. Follow the complete example's prerequisites, costs,
and ordered cleanup instructions. Do not deploy without authorization.

For the default DynamoDB integration, clean up the local-build deployment from the repository root with:

```sh
bash examples/complete/delete-zero-etl.sh examples/complete
task terraform -- destroy
```

### Offline verification

`task terraform-check` checks formatting of all repository Terraform sources and initializes/validates
`examples/complete` and `tests/acceptance/infra`, without cloud credentials. Individual resource/data-source snippets
receive formatting checks but are not standalone configurations. The complete example uses the local provider build;
acceptance fixtures use their pinned Registry release with development overrides disabled.

Acceptance fixture tests run native `terraform test` using `TF_CLI_CONFIG_FILE=/dev/null`, as described in the
[fixture guide](tests/acceptance/README.md). Existing provider Go contracts, including real resource lifecycles against
focused fake SQL catalogs, run through `task check`. None of these checks deploys
AWS resources. The `terraform` task remains available for normal local development-provider CLI commands.

The complete example uses blocking lifecycle postconditions on catalog and AWS readbacks. They remain enabled during
normal local and CI applies; `check` blocks are not used for release gates because their failures only warn.

Release example CI performs one apply, requires an empty plan, then runs the zero-ETL deletion helper and destroy.
It does not perform a second apply: optional update examples remain available locally, but are not live release-update
coverage. Terraform postconditions verify observed state, not row replication or every AWS policy's
effective authorization. Keep behavioral SQL queries for checks that require exercising access rather than metadata.

## Acceptance tests

Use the [live-test workflow guide](tests/acceptance/README.md) and the [complete example](examples/complete/README.md);
fixture prechecks live in `internal/provider/acc_test.go`. Live fixtures are independent of the complete example and
live in `tests/acceptance/infra`; their offline composition tests run via `task terraform-check`.

The [live-test workflow guide](tests/acceptance/README.md) describes the AWS fixture in the organization-managed `dev` environment,
the same Serverless/cluster acceptance suite on main and release, release-example validation and failed-cleanup recovery.
Pull requests never deploy it.

Acceptance fixture SQL is managed by an exactly pinned released provider, with development overrides disabled;
the Go harness supplies the development provider separately. Zero-ETL runs end-to-end only in the complete example.
Acceptance tests inherit the standard AWS credential chain: use `AWS_PROFILE` locally if needed, or environment
credentials as CI does. No acceptance-specific AWS profile setting or generated profile file is required.

- Direct Serverless tests additionally use `REDSHIFT_ACC_DIRECT=1`; provisioned tests need `REDSHIFT_ACC_CLUSTER` and
  `REDSHIFT_ACC_DB_USER`. Secret-mode tests use `REDSHIFT_ACC_SECRET_ARN` for Serverless and
  `REDSHIFT_ACC_CLUSTER_SECRET_ARN` for the provisioned cluster, so both suites can run in one process.
- Live Terraform assertions now cover every registered resource and data source. Feature-specific tests still require
  their own fixtures: COPY listings need a COPY source/role, and user-lockout listings require
  `REDSHIFT_ACC_USER_LOCKOUTS=1` plus a provisioned cluster with lockout security enabled.
- Prefer `verify-full`, with `REDSHIFT_ACC_CA_CERT_FILE` if needed; `REDSHIFT_ACC_SSLMODE=require` is for disposable fixtures.
- Use isolated, disposable warehouses/accounts and state. Register cleanup before mutations and verify restoration;
  never use production warehouses or existing directory memberships as fixtures.
- Isolated tests opt into `t.Parallel()`; run acceptance with `-parallel=8`. Test provider servers and SQL fixture/drift
  helpers share a per-warehouse mutation gate: catalog writes are exclusive; reads and independent lifecycles overlap.
  Shared-fixture and warehouse-setting tests stay sequential and finish before queued parallel tests start.
  Keep lifecycle steps ordered; parallelize subtests only when their objects are independent, including in shared
  Glue databases.
- Remove zero-ETL integrations before destination databases, and consumer associations before producer shares/warehouses.
  Large-table tests can load about 1 GiB; model metadata tests do not invoke Bedrock or train models.

### Run individual live tests

`TestAccLocalSQLLifecycle` needs credentials with privileges to create/drop databases and roles, create schemas, and
grant schema privileges. It creates uniquely named databases, schemas, roles, users, groups, memberships, and
object/system/default grants; checks no-change plans and imports; updates a grant; repairs drift; and verifies cleanup:

```sh
TF_ACC=1 \
REDSHIFT_ACC_REGION=eu-central-1 \
REDSHIFT_ACC_WORKGROUP=test-workgroup \
REDSHIFT_ACC_DATABASE=master \
go test ./internal/provider -run '^TestAccLocalSQLLifecycle$' -count=1 -parallel=8 -v -timeout=0
```

Run from the module directory with disposable test-account credentials. Without `TF_ACC=1`, live tests skip.
`AWS_PROFILE` is optional; the standard AWS credential chain also supports CI environment credentials.

Other useful tests:

- `TestAccAssumeroleGrantLifecycle` additionally needs `REDSHIFT_ACC_ASSUMEROLE=1`, an attached default IAM role, and
  a warehouse configured for identity-specific ASSUMEROLE control. It does not change warehouse-wide PUBLIC permissions.
- `TestAccCommentLifecycle` verifies database/schema/table/view/column annotations, imports, updates, drift repair,
  and comment-only deletion in a disposable database.
- `TestAccDirectIAMQueries`, `TestAccTransportSwitchLifecycle`, and `TestAccDirectPasswordPrivileges` cover direct TLS,
  transport changes, imports, and restricted password identities. Set `REDSHIFT_ACC_DIRECT=1` and ensure endpoint access.

Select one with `go test ./internal/provider -run '^<test-name>$' -count=1 -parallel=8 -v -timeout=0`.
Cross-account sharing and Identity Center need external prerequisites.
Set `REDSHIFT_ACC_CA_CERT_FILE` to an Amazon Trust Services PEM bundle if
the platform rejects the Serverless certificate; certificate-chain and hostname verification remain enabled.

Open enhancements, test limitations, and external acceptance prerequisites are tracked in [TODO.md](TODO.md).

## Documentation

Do not hand-edit generated `docs/`: edit templates, examples, or schema `MarkdownDescription`, then run `task docs`.
End-user guides belong under `docs/guides/`; contributor guidance stays at the repository root. Templates need
hand-aligned tables and prose within 120 columns; `task markdown-fmt` does not format them.

Templates must contain:

- Resource introductions follow this order: a description, a separate "For concepts and limitations, see AWS" paragraph
  linking relevant feature guides, and a separate "For SQL syntax, see AWS" paragraph linking statement references.
  Use a directly relevant broader guide when no dedicated feature guide exists; omit the concepts paragraph only when
  neither exists. Normalize existing feature links to this structure. Keep catalog references with the behavior they
  explain, not in the SQL syntax paragraph.
- Immediately after the schema, `## SQL Statements` with simplified SQL: one line per statement kind, identifying names,
  `...` for options. Add a short SQL comment before each statement explaining its purpose.
  Data sources name the catalog source.
- For resources, `## Reconciliation` immediately before `## Import`: a summary, a `| Change | Result |` table covering
  every argument and nested `block.field` in backticks, and a **Drift.** paragraph. Name the SQL operation, "no SQL", or
  replacement conditions. Keep ownership details under `## Lifecycle and Ownership`.
- Worked examples for non-trivial reconciliation, embedding existing SQL goldens via `codefile`, not copying them.
- For resources, import examples rendering `import.tf` first and `import.sh` second.

## Versioning and releases

Versions come only from `vX.Y.Z` tags. Before 1.0, minor releases may break schemas/behavior; patch releases must remain
compatible. From 1.0, breaking changes require a major version and state upgraders or documented migration.

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) for commits and PR titles (`!` for breaking
changes). Subjects become release notes, so describe user-visible effects. `.goreleaser.yml` defines release assets and
note grouping; `task snapshot` builds unsigned archives without publishing.

1. Confirm `main` CI and post-merge acceptance tests are green. The release workflow requires full acceptance coverage
   and the complete example's apply and no-change plan before publishing.
2. For a new series, update constraints in `README.md`, `examples/provider/provider.tf`, and
   `examples/complete/versions.tf`; regenerate docs and run project and Terraform checks.
3. Only when authorized, create and push an annotated `vX.Y.Z` tag on the intended `main` commit.
4. Verify the signed GitHub release, checksum signature, Registry version, and `terraform init`.
   Never reuse a published tag.

### Release signing key

Follow HashiCorp's [signing-key instructions](https://developer.hashicorp.com/terraform/registry/providers/publishing#preparing-and-adding-a-signing-key)
using a dedicated, passphrase-protected RSA 4096-bit key, not the default ECC type. Register the armored public key for
`netcheck-de`; store the private key and passphrase in Actions secrets `GPG_PRIVATE_KEY` and `PASSPHRASE`. The workflow
derives `GPG_FINGERPRINT`. Renew before expiry; rotation requires updating the Registry key and both secrets.
