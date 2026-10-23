# Development and releases

Use the AWS [Redshift SQL reference](https://docs.aws.amazon.com/redshift/latest/dg/c_SQL_commands.html) for syntax,
catalog behavior, and privileges. See [AGENTS.md](AGENTS.md) for repository layout and implementation guardrails.

## Local checks

Install Go matching `go.mod`, Terraform 1.14+, Task v3, `curl`, yamllint, and markdownlint-cli. The pinned golangci-lint
release is installed automatically. Run commands from the repository root.

| Command                                        | Purpose                                                                                     |
|------------------------------------------------|---------------------------------------------------------------------------------------------|
| `task check`                                   | Formatting, lint, vet, offline race tests with coverage, and generated documentation checks |
| `task test`                                    | Run all offline tests with race detection, coverage, and Go's default test-result caching   |
| `task fmt` / `task markdown-fmt`               | Format Go / repository Markdown                                                             |
| `task lint` / `task lint-fix`                  | Run the pinned Go linter / apply autofixes                                                  |
| `task golden`                                  | Rewrite SQL goldens; review every change                                                    |
| `task docs` / `task docs-check`                | Regenerate documentation / validate it and detect stale output                              |
| `task terraform-check` / `task terraform-test` | Validate examples / run mocked composition tests                                            |
| `task terraform -- <args>`                     | Run Terraform with the local provider; override `TF_DIR` if needed                          |
| `task snapshot`                                | Build unsigned release archives without publishing                                          |

Development files and build caches live in `.cache/`. Tools are pinned in `go.mod` (`go tool <name>`) or
`.golangci-lint-version`; lint configuration lives in `.golangci.yml`, `.markdownlint.yml`, and `.yamllint.yml`.
Refresh Go dependencies with `go get -u ./...` followed by `go mod tidy`; update a tool with
`go get -tool <module>@<version>`. Keep golangci-lint compatible with the Go version in `go.mod`.

## Go test conventions

- `go test ./...` includes unit tests and Terraform CLI contracts. Terraform must be installed; AWS credentials and
  warehouse connectivity are unnecessary. `task test` adds race detection and coverage. There are no build tags or
  separate execution modes for offline tests.
- Keep unit tests and shared fixtures in `*_test.go`; Terraform scenarios in neighboring `*_contract_test.go` files,
  named `TestContract...`, using `resource.UnitTest`. Fake SQL through `queryFunc` or the stateful `catalog`.
- Live tests use `*_acceptance_test.go`, `TestAcc...`, and `resource.Test`, gated by `testAccPreCheck` /
  `testAccWorkgroup` and `TF_ACC=1`. Skipped live tests are not verification.
- Use `t.Parallel()` with isolated fixtures only. Tests or parents that mutate environment variables, working
  directories, or shared globals stay serial. Register shared fixtures during package initialization.
- Use `testing/synctest` for in-process timeout, retry, and concurrency tests, not real sleeps or tiny deadlines.
  Virtual time does not control Terraform subprocesses or socket I/O.
- Use testify: `require` for preconditions and errors, `assert` for independent checks.
- Successful `resource.UnitTest` applies already check empty plans without and with refresh. Add explicit `PlanOnly`
  steps only for distinct behavior: changed configuration, drift, validation errors, failed applies, or import-only
  convergence.

## Adding resources and data sources

New types add files rather than modifying shared infrastructure. Follow this checklist:

1. Register the type in its own file with `registerResource(newX)` or `registerDataSource(newX)`.
2. Put pure renderers in `<type>_sql.go`: create, alter (using `alterStep`), drop, and catalog read. Resource methods
   execute through `resourceClient`; permission resources reuse `privilegeResource` and `grantSpec` where applicable.
3. Pin renderers with `checkSQL` and lifecycle transcripts with `runTranscripts`. Register replacement policy,
   data-source parity or an exemption, lifecycle cases, and a fake family in the type's test files.
4. Add a template and per-type examples, including resource import instructions, then run `task docs`.
5. Add usage to `examples/complete/<block>.tf`, data-source outputs to `outputs_<block>.tf`, and mocked assertions to
   `tests/<block>.tftest.hcl` using the shared mocks.
6. Update pinned type counts in `main_test.go`, provider-index tables and ownership entries in
   `templates/index.md.tmpl`, and any affected README or backlog entries. Run `task check` and the Terraform checks.

### Schema conventions

- Nested configuration uses singular blocks (`column`, `distribution`); lists of plain values use plural attributes.
  Required blocks use the matching `IsRequired()` validator, plus `SizeAtLeast(1)` for lists/sets. Their descriptions
  start with "At least one … is required." Blocks themselves have no `Required`, `Computed`, or `Default` fields.
- Absent list/set blocks are null. Block plan modifiers cannot add/remove elements; applied list order must match
  the plan.
- Data sources never declare blocks. `newCatalogDataSource` / `collectionElement` convert resource blocks to computed
  nested attributes. Lookup-only inputs go in `catalogSpec.selectors`; listing results use the data-source name.
  Use `lookupDescriptions` for resource wording that does not apply to lookups.
- Register replacement rules for every configurable attribute/block. Parity registration covers shared readable
  fields and lookup-only selectors; unpaired lookups use `standalone`, and resources without lookups need exemptions.
- Keep validation rules in one place: schema validators handle local constraints, while shared domain checks handle
  relationships between fields. Partial-known validation checks each rule once its own dependencies are known.
- Long-running resources use `operationTimeoutsBlock`, a resource-specific model with `timeouts.Value`, and
  `boundOperation` around Create/Update/Delete. Lookups and replacement/alter checks exclude the timeouts block.

### SQL and golden files

- Build SQL with `sqlclient.Stmt` / `Fragment` and catalog queries with `Select(...).Build()`, never concatenation.
  Quote values with the builder. Unquoted `Keyword` values must be constants, validated constructors (`OneOf`,
  `TypeName`, `ColumnType`, `Signature`), or reviewed `//sql:trusted` conversions. Configured SQL passes through
  `CheckUserSQL` and enters statements as `UserSQL` via `Verbatim`.
- Goldens live in `internal/provider/testdata/sql/<group>/<case>.sql`. Each group belongs to one renderer test or one
  transcript test because `task golden` removes stale files. Regeneration is refused when `CI` is set. Review changes
  against the AWS reference; never rewrite goldens merely to make a failing test pass.
- Keep SQL type names and keyword-like values uppercase in generated SQL, catalog readback, documentation, and
  examples. Preserve canonically equivalent configured spelling and never rewrite user-supplied SQL. Hand-written
  example SQL uses uppercase keywords/functions/casts; Terraform function names remain lowercase.
- Both transports wait for statement completion. Ordinary synchronous DROP needs no catalog readback. Delete checks
  existence with the resource's read, or with a minimal catalog read only where the full read issues extra queries.
  Keep missing-parent and warehouse ownership checks, and retain verification for permission reconciliation or
  asynchronous operations.

### Shared infrastructure

Feature blocks do not edit these shared files; a missing hook is a separate foundation change:

- `provider.go`, `registry.go`, `resource.go`, `privilege_resource.go`, `catalog_data_source.go`.
- Shared tests: `main_test.go`, `resource_test.go`, `golden_test.go`, `transcript_test.go`, `lifecycle_test.go`,
  `fake_catalog_test.go`, `replacement_policy_test.go`, `data_source_parity_test.go`, `documentation_contract_test.go`,
  `example_contract_test.go`, and `privilege_resource_test.go`.
- `examples/complete/tests/composition.tftest.hcl`, `examples/complete/outputs.tf`, `templates/index.md.tmpl`,
  `README.md`, `TODO.md`, and `go.mod`.

After parallel feature blocks merge, one integration change updates shared counts, tables, examples, and backlog.

## Terraform example tests

The complete example lives in `examples/complete`; small per-type snippets feed generated documentation.
`task terraform-test` builds the local provider, initializes Terraform without a backend, and runs mocked applies.
AWS, Redshift, and Random are mocked; no cloud credentials are required. Each suite explicitly references its mocks
under `tests/mocks/` because Terraform does not share mock-provider declarations between test files.

```sh
task terraform-test
task terraform-test -- -filter=tests/composition.tftest.hcl -verbose
```

Override `TF_DIR` to use another configuration. These tests check example composition; Go tests check provider
behavior, and live acceptance tests check Redshift itself.

## Connection acceptance tests

Start with the [acceptance instructions](README.md#acceptance-test) and the
[complete example](examples/complete/README.md). Fixture selectors and prechecks live in `internal/provider/acc_test.go`
and the relevant `*_acceptance_test.go` files.

- Direct Serverless tests additionally use `REDSHIFT_ACC_DIRECT=1`; provisioned tests need `REDSHIFT_ACC_CLUSTER` and
  `REDSHIFT_ACC_DB_USER`. Secret-mode tests use `REDSHIFT_ACC_SECRET_ARN`.
- Prefer `verify-full`. Use `REDSHIFT_ACC_CA_CERT_FILE` for a trusted CA bundle; `REDSHIFT_ACC_SSLMODE=require` encrypts
  without authenticating the server and is only suitable for disposable fixtures.
- Keep warehouse/account environments and Terraform states isolated. Register cleanup before mutations, verify final
  inventories, and never use shared production warehouses or existing directory memberships as fixtures.
- Destructive opt-ins, including integration mutations and system-setting probes, require disposable fixtures and
  restoration checks. Remove zero-ETL integrations before destination databases, and consumer associations before
  producer shares and warehouses. Large-table tests can load about 1 GiB; model metadata tests do not invoke Bedrock
  or train models.

## Documentation

Never edit generated `docs/` directly. Change `templates/`, per-type examples, or schema `MarkdownDescription`, then
run `task docs`. `task docs-check` validates Registry layout and regenerates pages to detect missing or stale output.
Publish end-user guides under `docs/guides/`; keep contributor guidance at the repository root.

Resource templates must contain:

- `## SQL Statements` after the introduction: a short SQL block with one line per statement kind, identifying
  attribute names, and `...` for options. Data-source templates use `## Catalog Query` with the catalog source.
- `## Reconciliation` immediately before `## Import`: an opening summary; a `| Change | Result |` table naming every
  configurable argument and nested `block.field` in backticks, including write-only fields, triggers, and timeouts;
  and a **Drift.** paragraph covering readback and its limits. Results name the SQL operation, "no SQL", or replacement
  and its conditions. Ownership/permissions belong under `## Lifecycle and Ownership`.
- Worked examples for non-trivial reconciliation, embedding SQL from existing goldens with
  `{{ codefile "sql" "internal/provider/testdata/sql/<group>/<case>.sql" }}`, not hand-written copies.

Derive reconciliation claims from plan modifiers, replacement policies, alter steps, Update/Read, and goldens.
Documentation contract tests enforce input coverage and section structure. Templates are not formatted by
`task markdown-fmt`: align their tables by hand and keep prose within 120 columns. Generated pages follow the
[Terraform Registry guidelines](https://developer.hashicorp.com/terraform/registry/providers/docs).

## GitHub Actions

`ci.yml` runs Go/project lint, one offline Go race/coverage job with Terraform installed, example validation and mocked
tests, documentation checks, and unsigned platform builds. These checks need no organization secrets or cloud
credentials. CI uses the same local tasks, plus the lint and GoReleaser actions.

`release.yml` accepts only `vMAJOR.MINOR.PATCH` tags on `main`, runs full CI, then publishes signed assets. Only the
publishing job has repository write permission. Dependabot proposes weekly Go module/tool and Actions updates.

## Terraform Registry publication

The provider address is `netcheck-de/redshift`. `.goreleaser.yml` builds Linux/macOS/Windows ZIPs for amd64/arm64,
SHA256 checksums, a detached GPG signature, and a protocol-6 manifest. The Registry ingests signed GitHub releases;
`task snapshot` builds unsigned archives locally without publishing.

### Versioning and releases

The SemVer tag is the only release version source; local builds use the newest tag. Before 1.0, minor releases may
break schemas/behavior; patch releases contain compatible additions/fixes. From 1.0, breaking changes require a major
version and state upgraders or documented migration.

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) for commits and PR titles; `!` marks breaking
changes. Release notes use commit subjects since the previous tag: merge commits preserve individual entries, while
squashing uses the squash subject. `.goreleaser.yml` defines grouping and exclusions; the PR-title check validates
titles but does not determine the merge strategy.

1. Confirm `main` CI is green; run live acceptance tests when warranted by the changes.
2. For a new series, update provider constraints in `README.md`, `examples/provider/provider.tf`, and
   `examples/complete/versions.tf`; regenerate docs and run `task check` plus Terraform checks.
3. When explicitly authorized, create and push a new annotated `vX.Y.Z` tag on the intended `main` commit.
4. Confirm the signed GitHub release, checksum signature, Registry version, and a successful `terraform init`.
5. Never move or reuse a published tag; fix a release with a new patch version.

### Create and configure the release signing key

Follow HashiCorp's [signing-key instructions](https://developer.hashicorp.com/terraform/registry/providers/publishing#preparing-and-adding-a-signing-key).
Use a dedicated, passphrase-protected RSA 4096-bit signing key; the Registry does not accept the default ECC type.
Register the armored public key for the `netcheck-de` namespace and configure repository Actions secrets
`GPG_PRIVATE_KEY` and `PASSPHRASE`. The workflow derives `GPG_FINGERPRINT`; no separate fingerprint secret is needed.

Never commit private keys or passphrases. Export outside the repository with restrictive permissions; protect backups
and the revocation certificate. Verify published checksums with `gpg --verify` and the expected fingerprint. Before
expiry, renew/re-export and update the Registry public key and GitHub private-key secret; rotation requires updating
the registered public key and both repository secrets.
