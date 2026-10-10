# AGENTS.md

Terraform provider `netcheck-de/redshift` (terraform-plugin-framework, protocol 6) that manages Redshift SQL objects
through the Data API or a direct TLS connection.

## Layout

- `internal/provider`: resources, data sources, provider config (`provider.go`, `connection*.go`).
- `internal/sqlclient`: transport-neutral `Client` interface, statement and catalog query builders, mutation
  serialization.
- `internal/redshiftdata`, `internal/redshiftconn`: Data API and direct pgx transports.
- `templates/`, `examples/{provider,resources,data-sources}`: sources for the generated `docs/`.
- `examples/complete`: end-to-end example that uses every resource and data source; `tests/` holds mocked
  `terraform test` runs.

## Commands

- `task check`: lint, Markdown format check, race tests with coverage, docs check. Run before finishing.
- `task lint` / `task lint-fix` download and use the golangci-lint release pinned in `.golangci-lint-version`.
- `task docs`: regenerate `docs/`. `task terraform-check` and `task terraform-test`: validate and test the examples.
- Tools (`tfplugindocs`, `mdox`, `actionlint`) are `tool` directives in `go.mod`; run them with `go tool <name>`.

## Rules

- Never edit `docs/` by hand. Change schema `MarkdownDescription`, `templates/`, or `examples/`, then run `task docs`.
- Resource code talks to Redshift only through `sqlclient.Client`; depguard forbids importing transports or AWS
  service clients outside `provider.go` and `connection.go`.
- Build statements with `sqlclient.Stmt`/`Fragment` and catalog reads with `sqlclient.Select(...).Build()`; never
  concatenate SQL. Unquoted text is a `Keyword` (a constant, `OneOf`, `TypeName`, `ColumnType`, `Signature`, or a
  `//sql:trusted` conversion); configured SQL is `UserSQL` from `CheckUserSQL`. `trusted_sql_test.go` enforces this.
- Follow the AWS Redshift SQL reference for syntax and catalog behavior; catalog privilege names may differ from
  configuration names (see `normalizePrivilege`).
- Validate a tuple before the first `State.Set` in Create, and expose the same check in `ValidateConfig`.
- Create and Update re-read the catalog to verify convergence; Read calls `RemoveResource` when the object or its parent
  is gone, and returns errors only for real failures.
- Resource IDs are JSON objects built by `resourceClient.identity` and checked by `bound`; imports use the same JSON.
- Nested configuration uses blocks with singular names; a required block validates with `listvalidator.IsRequired()`,
  `setvalidator.IsRequired()`, or `objectvalidator.IsRequired()` (plus `SizeAtLeast(1)` for lists and sets) and its
  description starts with "At least one … is required". Lists of plain values stay plural attributes. Data sources never declare blocks, and a listing's result attribute is named after
  the data source. See "Schema conventions" in `DEVELOPMENT.md`.
- Comments explain why, not what.

## Tests

- testify only: `require` for preconditions and errors, `assert` for independent checks.
- Offline tests fake SQL with `queryFunc` (`resource_test.go`) or the `catalog` fake (`fake_catalog_test.go`).
- Golden files under `testdata/sql/` pin all SQL (`checkSQL` for renderers, `runTranscripts` for lifecycle calls);
  `task golden` rewrites them, and every changed file needs a reason.
- Acceptance tests use `resource.Test`, gated by `testAccPreCheck`/`testAccWorkgroup` (`acc_test.go`) and `TF_ACC=1`.

## Adding a resource or data source

New types only add files; the frozen shared files are listed in `DEVELOPMENT.md`, and a missing hook there is a
separate foundation change.

1. Register the type in its own file: `var _ = registerResource(newX)` or `registerDataSource(newX)`.
2. Put pure renderers in `<type>_sql.go` (`create<X>Statement`, `alter<X>Statements` via `alterStep`,
   `drop<X>Statement`, `read<X>Query`). Pin them in `<type>_sql_test.go` with `checkSQL` goldens under
   `testdata/sql/<type>/`; record with `task golden` and check each file against the AWS page.
3. In the type's tests, call `registerReplacementPolicy`, `registerParity` (or its exemption), and
   `registerLifecycleCase`; teach the `catalog` fake with `registerFakeFamily` in `fake_<type>_test.go`.
4. Add `templates/<kind>/<name>.md.tmpl`, plus `examples/resources/redshift_<name>/{resource.tf,import.sh}` or
   `examples/data-sources/redshift_<name>/data-source.tf`, then run `task docs`. The template opens with a ```sql block
   of simplified statements (statement kind, identifying names, `...` for options; data sources: the catalog source),
   as described in `DEVELOPMENT.md`.
5. In `examples/complete`, use it in the block's own `<block>.tf`, expose each data source in `outputs_<block>.tf`, and
   assert it in `tests/<block>.tftest.hcl`, which declares the shared mocks from `tests/mocks/`.

## Changes and releases

- PR titles (the squash-merge subject) must follow Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `ci:`,
  `chore:`, …; `!` for breaking changes); use the same style for commits. They become the release notes, so describe
  the user-visible effect.
- Versions come only from `vX.Y.Z` tags; follow "Versioning and releases" in `DEVELOPMENT.md`. Never tag or push
  unless asked.
