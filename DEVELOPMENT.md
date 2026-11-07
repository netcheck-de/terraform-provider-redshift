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
|   |   `-- tests/      # Mocked composition tests
|   |       `-- mocks/  # Shared provider mocks
|   |-- data-sources/   # Data-source examples
|   |-- provider/       # Provider configuration examples
|   `-- resources/      # Resource and import examples
|-- internal/
|   |-- provider/       # Resources, data sources, and provider wiring
|   |-- redshiftconn/   # Direct pgx transport
|   |-- redshiftdata/   # Data API transport
|   `-- sqlclient/      # Client interface, SQL builders, mutation serialization
`-- templates/          # Documentation templates
```

## Local checks

Install Go matching `go.mod`, Terraform 1.16+, Task v3, `curl`, yamllint, and markdownlint-cli
(`brew install yamllint markdownlint-cli`). Run commands from the repository root.

| Command                                        | Purpose                                                  |
|------------------------------------------------|----------------------------------------------------------|
| `task check`                                   | Lint, formatting, offline race tests, coverage, and docs |
| `task test`                                    | Offline tests with race detection and coverage           |
| `task fmt` / `task markdown-fmt`               | Format Go / Markdown                                     |
| `task lint` / `task lint-fix`                  | Go lint / autofix                                        |
| `task golden`                                  | Regenerate SQL goldens                                   |
| `task docs` / `task docs-check`                | Generate / validate documentation                        |
| `task terraform-check` / `task terraform-test` | Validate / test examples                                 |
| `task terraform -- <args>`                     | Local provider CLI; `TF_DIR` selects the configuration   |

Builds and caches live in `.cache/`. Task installs the golangci-lint version in `.golangci-lint-version`; other pinned
tools are `tool` directives in `go.mod`, invoked with `go tool <name>`.

Run `task check` before submitting changes; also run `task terraform-check` and `task terraform-test` when examples change.

## Architecture and lifecycle

- Resources access Redshift through `sqlclient.Client`; transport and AWS service clients belong in provider wiring.
- Validate tuples before the first `State.Set` in Create, and expose the same check in `ValidateConfig`.
- Create/Update re-read the catalog to verify convergence. Read calls `RemoveResource` when the object or its parent
  is gone; genuine query failures remain errors.
- Synchronous DROP needs no post-delete refresh. Use only checks needed for safe deletion; retain convergence checks
  for permission reconciliation and asynchronous operations.

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
   fake's `registerFakeFamily` in the type's tests. Cover every configurable input in the replacement policy.
4. Add `templates/<kind>/<name>.md.tmpl` and per-type examples. Resource examples include `resource.tf`, `import.tf`, and
   `import.sh`; data sources use `data-source.tf`. Follow the [documentation format](#documentation).
5. Extend `examples/complete/<block>.tf`, `outputs_<block>.tf`, and `tests/<block>.tftest.hcl`, using shared mocks.
   Update the type tables and ownership matrix in `templates/index.md.tmpl`, then run `task check`,
   `task terraform-check`, and `task terraform-test`.

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

`task terraform-test` builds the local provider and runs mocked applies in `examples/complete`, without cloud credentials.
Each suite must declare its mocks from `tests/mocks/`; Terraform does not share declarations between test files.

```sh
task terraform-test -- -filter=tests/composition.tftest.hcl -verbose
```

Override `TF_DIR` to use another configuration. These tests check composition, not SQL behavior.

## Connection acceptance tests

Start with the [acceptance instructions](README.md#acceptance-test) and the
[complete example](examples/complete/README.md); fixture prechecks live in `internal/provider/acc_test.go`.

- Direct Serverless tests additionally use `REDSHIFT_ACC_DIRECT=1`; provisioned tests need `REDSHIFT_ACC_CLUSTER` and
  `REDSHIFT_ACC_DB_USER`. Secret-mode tests use `REDSHIFT_ACC_SECRET_ARN`.
- Prefer `verify-full`, with `REDSHIFT_ACC_CA_CERT_FILE` if needed; `REDSHIFT_ACC_SSLMODE=require` is for disposable fixtures.
- Use isolated, disposable warehouses/accounts and state. Register cleanup before mutations and verify restoration;
  never use production warehouses or existing directory memberships as fixtures.
- Remove zero-ETL integrations before destination databases, and consumer associations before producer shares/warehouses.
  Large-table tests can load about 1 GiB; model metadata tests do not invoke Bedrock or train models.

## Documentation

Do not hand-edit generated `docs/`: edit templates, examples, or schema `MarkdownDescription`, then run `task docs`.
End-user guides belong under `docs/guides/`; contributor guidance stays at the repository root. Templates need
hand-aligned tables and prose within 120 columns; `task markdown-fmt` does not format them.

Templates must contain:

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

1. Confirm `main` CI is green and run live acceptance tests when warranted.
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
