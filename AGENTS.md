# AGENTS.md

Terraform provider `netcheck-de/redshift`: terraform-plugin-framework, protocol 6, Redshift Data API or direct TLS.

Read and follow [DEVELOPMENT.md](DEVELOPMENT.md) as the project's instruction set for workflows, conventions, and releases.

## Layout

- `internal/provider`: resources, data sources, and provider wiring; `internal/sqlclient`: SQL builders and client interface.
- `internal/redshiftdata`, `internal/redshiftconn`: Data API and direct pgx transports.
- `templates/` and `examples/`: sources for generated `docs/`; `examples/complete/tests/`: mocked composition tests.

## Commands

- Run `task check` before finishing; also run `task terraform-check` and `task terraform-test` when examples change.
- Run `task docs` after changing documentation sources.

## Guardrails

- Never hand-edit generated `docs/`; change schemas, templates, or examples.
- Resources use only `sqlclient.Client`; transport and AWS service clients belong in provider wiring.
- Build SQL with `sqlclient.Stmt`/`Fragment` and `Select(...).Build()`, never concatenation. Follow the AWS Redshift
  reference, not PostgreSQL assumptions.
- Validate tuples before the first `State.Set` in Create; expose the same check in `ValidateConfig`.
- Create/Update verify catalog convergence. Read removes state when the object or parent is gone; genuine failures
  remain errors.
- Synchronous DROP needs no post-delete refresh. Keep only checks needed for safe deletion; retain convergence checks
  for permissions and asynchronous operations.
- Test provider behavior, not framework internals. Do not weaken production code to accommodate invalid synthetic tests.
- Keep `TODO.md` limited to open work. Never tag or push unless explicitly asked.
