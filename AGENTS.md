# AGENTS.md

Terraform provider `netcheck-de/redshift` (terraform-plugin-framework, protocol 6) that manages Redshift SQL objects
through the Data API or a direct TLS connection.

## Layout

- `internal/provider`: resources, data sources, provider config (`provider.go`, `connection*.go`).
- `internal/sqlclient`: transport-neutral `Client` interface, `Identifier`/`Literal` quoting, mutation serialization.
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
- Build SQL with `sqlclient.Identifier`/`Literal` or named `:param` bindings; never interpolate raw values.
- Follow the AWS Redshift SQL reference for syntax and catalog behavior; catalog privilege names may differ from
  configuration names (see `normalizePrivilege`).
- Validate a tuple before the first `State.Set` in Create, and expose the same check in `ValidateConfig`.
- Create and Update re-read the catalog to verify convergence; Read calls `RemoveResource` when the object or its parent
  is gone, and returns errors only for real failures.
- Resource IDs are JSON objects built by `resourceClient.identity` and checked by `bound`; imports use the same JSON.
- Comments explain why, not what.

## Tests

- testify only: `require` for preconditions and errors, `assert` for independent checks.
- Offline tests fake SQL with `queryFunc` (`resource_test.go`) or the `catalog` fake (`fake_catalog_test.go`).
- Acceptance tests use `resource.Test`, gated by `testAccPreCheck`/`testAccWorkgroup` (`acc_test.go`) and `TF_ACC=1`.

## Adding a resource or data source

1. Register it in `provider.go`, and update the type counts in `main_test.go`.
2. Add cases to `replacement_policy_test.go`, `data_source_parity_test.go`, and, where the fake supports it,
   `lifecycleCases` in `lifecycle_test.go`.
3. Add `templates/<kind>/<name>.md.tmpl`, plus `examples/resources/redshift_<name>/{resource.tf,import.sh}` or
   `examples/data-sources/redshift_<name>/data-source.tf`, then run `task docs`. The template opens with a ```sql block
   of simplified statements (statement kind, identifying names, `...` for options; data sources: the catalog source),
   as described in `DEVELOPMENT.md`.
4. Use it in `examples/complete`, expose each data source in `outputs.tf`, and assert it in
   `tests/composition.tftest.hcl`.

## Changes and releases

- PR titles (the squash-merge subject) must follow Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `ci:`,
  `chore:`, …; `!` for breaking changes); use the same style for commits. They become the release notes, so describe
  the user-visible effect.
- Versions come only from `vX.Y.Z` tags; follow "Versioning and releases" in `DEVELOPMENT.md`. Never tag or push
  unless asked.
