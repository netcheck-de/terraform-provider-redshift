# Development and releases

## SQL implementation reference

The AWS [Redshift SQL commands reference](https://docs.aws.amazon.com/redshift/latest/dg/c_SQL_commands.html) is the
source of truth for supported objects, syntax, privileges, and behavior when implementing or extending existing or new
resources. Follow its command-specific documentation and verify catalog and lifecycle behavior against Redshift.

## Local checks

Use Go matching `go.mod`, Terraform 1.14 or later, Task v3, and `curl`; golangci-lint is fetched automatically. Run
`task check` from the module root to check gofmt formatting, lint, `go vet`, and offline race tests with statement
coverage printed to the terminal. Tests check that every resource and data source has a generated documentation
page, a template, and examples, and that every resource template documents how each argument reconciles. A
golangci-lint `depguard` rule prevents resource code from importing concrete transports.
[Live acceptance tests](README.md#acceptance-test) are opt-in.

The Taskfile provides `fmt`, `markdown-fmt`, `markdown-fmt-check`, `lint`, `actionlint`, `test`, `golden`, `check`,
`docs`, `docs-check`, `build`, `terraform`, `terraform-check`, `terraform-test`, and `snapshot`. The Terraform runner defaults to
`examples/complete`; override `TF_DIR` for another configuration. Cache and development overrides live in the ignored
`.cache/` directory.

Development tools (`tfplugindocs`, `mdox`, `actionlint`) are pinned with `tool` directives in `go.mod`; tasks run them
with `go tool <tool>`. They are never linked into the provider binary. Refresh dependencies with `go get -u ./...`
followed by `go mod tidy`, and upgrade a tool explicitly with `go get -tool <module>@<version>`.

## Go test conventions

[testify](https://github.com/stretchr/testify) is the required assertion library for all Go tests. Use `require` when a
failure makes the rest of the test meaningless (setup, errors, preconditions) and `assert` for independent checks; do not
hand-roll `if ... { t.Errorf(...) }` comparisons. golangci-lint's `testifylint` enforces idiomatic usage.

- Keep tests next to the code they cover (`x.go` / `x_test.go`). Cross-cutting invariants that span every resource live
  in dedicated files: `replacement_policy_test.go`, `data_source_parity_test.go`, `lifecycle_test.go`,
  `documentation_contract_test.go`, and `example_contract_test.go`.
- Offline tests fake SQL through `queryFunc` (`resource_test.go`) or the stateful `catalog` fake
  (`fake_catalog_test.go`); they never contact AWS.
- Acceptance tests use `terraform-plugin-testing` with `resource.Test`, gated by `testAccPreCheck` / `testAccWorkgroup`
  in `acc_test.go`.

## Adding resources and data sources

The provider grows in parallel work blocks, so a new type adds files instead of editing shared ones.

- **Registration.** Each type file registers itself with `var _ = registerResource(newX)` or
  `var _ = registerDataSource(newX)` (`registry.go`); `provider.go` returns clones of the registry, and `main_test.go`
  pins the exact resource and data-source counts and checks them against the generated `docs/` pages. The type's test
  files register its cross-cutting cases next to the code they cover:
  - `registerReplacementPolicy(type, rules)` declares, per attribute and block, whether a change never, always, or
    conditionally replaces the object; `replacement_policy_test.go` checks the schema against it, and every block
    counts as an input.
  - `registerParity(parityCase{...})` pairs a lookup with its resource (or a collection lookup with its element shape);
    a resource without a lookup registers an explicit exemption. `lookupSelectors` names inputs only the lookup has.
  - `registerLifecycleCase(...)` adds the type to the lifecycle, retry, and transcript runs in `lifecycle_test.go`.
  - `registerFakeFamily(...)` in `fake_<type>_test.go` teaches the stateful `catalog` fake the type's statements and
    catalog reads; the fake consults registered families before its built-in cases.
- **Schema conventions.** Similar resources model the same concept the same way:
  - Nested configuration is a block with a singular name (`column`, `primary_key`, `distribution`), as in the
    official providers; lists of plain values stay plural attributes (`arguments`, `search_path`, `values`).
  - Blocks have no `Required`, `Computed`, or `Default`. A required list block declares `listvalidator.IsRequired()`
    with `listvalidator.SizeAtLeast(1)`, a required set block `setvalidator.IsRequired()` with
    `setvalidator.SizeAtLeast(1)`, and a required single block `objectvalidator.IsRequired()`. Its description starts
    with "At least one `x` block is required." because tfplugindocs labels every block as optional; lookups drop
    that sentence. Attributes inside a block may be optional and computed and may have defaults.
  - Absent list and set blocks arrive as null, block plan modifiers cannot add or remove elements, and Terraform
    requires the applied state of a list block to keep the planned order.
  - Data sources never declare blocks. `newCatalogDataSource` and `collectionElement` turn each resource block into a
    computed nested attribute of the same name and type, so `data.redshift_x.y.column[0].name` addresses what the
    resource does and a single block reads as an object without `[0]`. An input that only the lookup needs, such as a
    plain list of types selecting an overload, is a `catalogSpec.selectors` entry.
  - A listing's result attribute is named after the data source: `data.redshift_tables.x.tables`.
  - A resource whose operations can outlast one statement declares `timeoutsBlockName: operationTimeoutsBlock(...)`,
    embeds its lookup-shared model in a `<type>ResourceModel` with a `Timeouts timeouts.Value` field, and wraps
    Create, Update, and Delete in `boundOperation`. Lookups, replacement policies, and alter coverage skip the block
    by name, and `testState` leaves it null for models without it.
  - `lookupDescriptions` replaces resource wording that does not apply to an observed value; dotted paths such as
    `distribution.style` reach nested attributes.
- **Renderers.** `<type>_sql.go` holds pure functions from the Terraform model to SQL: `create<X>Statement`,
  `alter<X>Statements` (one statement per changed option, built from `alterStep`s in `alter.go`), `drop<X>Statement`,
  and `read<X>Query`. Resource methods only call them and run the result through `resourceClient.exec` and
  `selectRows`. Permission types reuse `privilegeResource` with a `prepare` function returning a `grantSpec`; set
  `grantOptions` to add the `grant_option_privileges` subset for user grantees, and `recipient` for grantees that are
  not SQL identities, such as `RLS POLICY`.
- **Golden files.** `<type>_sql_test.go` pins every renderer with `checkSQL(t, group, cases)`, and lifecycle runs pin
  the full SQL conversation with `runTranscripts`. Files live under `internal/provider/testdata/sql/<group>/<case>.sql`:
  statements end with `;` and are separated by blank lines, `-- no statements` and `-- error: …` record empty and failed
  renders, and transcripts add `-- database:` and `-- params:` lines. `task golden` rewrites them (refused when `CI` is
  set) and removes stale files. Because of that cleanup, a group belongs to one test: one `checkSQL` call, or the
  `checkTranscript` calls of one test. Review each new file against the AWS command page, and justify every changed
  one.
- **Statement builder.** `sqlclient.Stmt(verb)` and `Fragment()` build statements from quoted values (`Ident`,
  `Qualified`, `Lit`, `Int`, `Bool`, `JSON`, `Body`) and trusted text. Every builder method returns a copy, so a shared
  prefix can be extended per option. Text that is emitted unquoted is a `sqlclient.Keyword`: a constant, the result
  of `OneOf` (an allowlist), `TypeName` (a validated, canonical Redshift type), `ColumnType` (the same for a column,
  with the default length or precision the server applies), or `Signature`, or a conversion annotated `//sql:trusted`
  after review. Configured SQL such as view queries, defaults, and predicates is a `UserSQL` from
  `CheckUserSQL` and enters a statement only through `Verbatim`. `trusted_sql_test.go` fails on unannotated
  conversions. Catalog reads use `sqlclient.Select(...).From(...).Where(...)`, whose `Build` keeps `:name` bindings in
  sync with the conditions and rejects empty values.
- **Frozen files.** Blocks do not edit `provider.go`, `registry.go`, `resource.go`, `privilege_resource.go`,
  `catalog_data_source.go`, the shared test files (`main_test.go`, `resource_test.go`, `golden_test.go`,
  `transcript_test.go`, `lifecycle_test.go`, `fake_catalog_test.go`, `replacement_policy_test.go`,
  `data_source_parity_test.go`, `documentation_contract_test.go`, `example_contract_test.go`,
  `privilege_resource_test.go`), `examples/complete/tests/composition.tftest.hcl`, `examples/complete/outputs.tf`,
  `templates/index.md.tmpl`, `README.md`, `TODO.md`, or `go.mod`. A block that needs a new hook reports it, and the hook
  lands as a separate foundation change first. After the blocks merge, one integration change raises the pinned
  counts in `main_test.go` and updates the type tables and ownership matrix in `templates/index.md.tmpl`, `README.md`,
  and `TODO.md`.
- **Examples.** A block adds its objects to `examples/complete/<block>.tf` (access control lives in
  `access_users.tf`, `access_roles.tf`, and `access_grants.tf`), exposes each lookup in `outputs_<block>.tf`, and
  asserts them in `tests/<block>.tftest.hcl`. `example_contract_test.go` requires every type in the example and every
  lookup in an `outputs*.tf` file.

## Terraform example tests

The complete example's configuration tests live in `examples/complete/tests/`: `composition.tftest.hcl` covers the
shared composition, and one `<block>.tftest.hcl` per work block holds that block's runs. Run them from the provider
root:

```sh
task terraform-test
task terraform-test -- -filter=tests/composition.tftest.hcl -verbose
```

The task builds the local provider, initializes `TF_DIR` without a backend, and runs `terraform test`. The suites mock
both AWS aliases, all eight Redshift aliases, and Random, so no AWS credentials or warehouse connectivity are needed.
Mock values live in `tests/mocks/<provider>[_<alias>]/*.tfmock.hcl` and every suite references them with
`mock_provider "<provider>" { source = "./tests/mocks/..." }`, because Terraform does not share mock providers between
test files. Five composition runs cover self-provisioning private defaults, ASSUMEROLE disabled, optional SSO,
cross-account sharing, and all connection probes, including their policy and lookup output wiring.

Runs use mocked `apply` operations because Terraform defers dependent lookup reads until apply; deterministic overrides
provide their output values. Terraform manages isolated test state and tears down the mocked objects. The assertions test
the example's HCL composition; Go tests cover provider behavior, and live acceptance tests cover actual Redshift.

The runnable end-to-end example is `examples/complete`. `examples/provider`, `examples/resources`, and
`examples/data-sources` hold the small per-type snippets embedded in the generated documentation.
The complete README includes its ASCII architecture overview, real apply/destroy instructions, and optional features.

## Connection acceptance tests

Serverless direct-connection tests (see the [connection methods guide](docs/guides/connection_methods.md)) use the
standard acceptance environment plus `REDSHIFT_ACC_DIRECT=1`:

- `TestAccDirectIAMQueries`: authenticated TLS and named-parameter/result handling without mutations.
- `TestAccTransportSwitchLifecycle`: Data API create, direct IAM no-change plan/import/update, transport switch back,
  and direct cleanup in an isolated database.
- `TestAccDirectPasswordPrivileges`: a disposable non-superuser password identity can read its granted fixture but
  cannot insert; the fixture database and user are removed afterwards.
- `TestAccLanguageGrantPublicDefault`: a disposable non-superuser can create a procedure until a `PUBLIC`
  `redshift_language_grant` without `USAGE` revokes the built-in default, and still cannot after destroy.

Set `REDSHIFT_ACC_SSLMODE` (for example `require`) to override the default `verify-full` for direct tests; on macOS
this is needed for Serverless endpoints, whose certificates lack Certificate Transparency timestamps.

`TestAccClusterTransportLifecycle` additionally requires `REDSHIFT_ACC_CLUSTER` and `REDSHIFT_ACC_DB_USER`, with a
provisioned test warehouse and an existing privileged SQL identity. `REDSHIFT_ACC_SECRET_ARN` optionally adds a
secret-mode Data API switch; `REDSHIFT_ACC_DIRECT=1` additionally switches to direct cluster IAM. These fixtures are not
created by CI.

Provisioned cluster and Secrets Manager tests require separately configured test fixtures. Cross-account reader testing
requires producer/consumer fixture connectivity in addition to the normal lifecycle suite.

## Lint policy

`task markdown-fmt` formats the root Markdown files and example READMEs with the pinned mdox tool.
`task markdown-fmt-check` checks the same files without rewriting them and runs in `task check` and CI. Generated
`docs/` pages are excluded because `tfplugindocs` owns their formatting; generated schema sections are wrapped in
markdownlint disable/enable comments for line length and inline anchors. The formatter
aligns table columns and preserves existing prose line breaks. Markdownlint checks the 120-column prose limit
separately.

`.golangci-lint-version` pins the exact golangci-lint release for CI (installed by the action) and local runs:
`task lint` installs that release once into `.cache/tools/golangci-lint/<version>/` with the official installer from
the same release tag, which verifies the checksum, and reuses it afterwards. `task lint-fix` applies autofixes. Bumping
the version file upgrades both; choose a release built with a Go version at least as new as the `go` directive in
`go.mod`.

`task fmt` runs gofmt. `task lint` and `task check` enforce gofmt formatting and run govet through golangci-lint, as do
CI and the release gate.

`.golangci.yml` enables standard correctness checks plus selected maintainability rules:

| Rule                                                                 | Purpose                                                                           |
|----------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Standard `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused` | Error handling, compiler-adjacent checks, ineffective assignments, and dead code. |
| `errorlint`, `nilerr`                                                | Wrapped-error handling and consistent error propagation.                          |
| `durationcheck`                                                      | Accidental duration multiplication.                                               |
| `gocritic`, `unconvert`                                              | Suspicious constructs and unnecessary conversions.                                |
| `revive`                                                             | Exported/private-receiver API documentation, package comments, and Go naming.     |
| `misspell`                                                           | Spelling in comments and text.                                                    |
| `testifylint`, `thelper`                                             | Appropriate assertions and helper attribution.                                    |
| `predeclared`                                                        | Shadowing Go's built-in identifiers.                                              |
| `nolintlint`                                                         | Well-formed suppression directives.                                               |

Mandatory parallel tests conflict with environment/global-state fixtures. Generic complexity and duplication thresholds
add noise to Terraform lifecycle handlers. SQL mutations use domain-specific quoting and privilege allowlists rather
than a blanket string-construction prohibition. Reassess rules when concrete defects justify them.

## Documentation

Registry pages in `docs/` are generated; do not edit them directly. Edit the templates in `templates/`, the examples
in `examples/provider/`, `examples/resources/`, and `examples/data-sources/`, or the schema `MarkdownDescription`
strings, then run `task docs` (or `go generate ./...`) to regenerate them with the pinned `tfplugindocs`.

After its introduction, every resource template has a `## SQL Statements` section holding only a short `sql` block of
simplified statements: one line per statement kind the resource issues (`CREATE`, `ALTER`, `DROP`, `GRANT`, `REVOKE`,
`COMMENT`, …), with only the identifying Terraform attribute names and `...` for all further options. Data source
templates show the catalog source under `## Catalog Query` as `SELECT ... FROM <view> WHERE <key> = '...';` or the
`SHOW` command. Because options are elided, adding attributes or clauses keeps the block valid; update it only when a
type starts or stops issuing a statement kind or reads a different catalog source.

Every resource template also has a `## Reconciliation` section after the usage and explanatory sections and right
before `## Import`. It is the one place that explains changes: in-place and replacement notes belong there, while
ownership and permission prose stays under `## Lifecycle and Ownership`. The section has these parts:

- One opening sentence with the general rule: what refresh reads, which statements an in-place change runs before the
  catalog is re-read, and that every other change replaces the object.
- A hand-aligned `| Change | Result |` table with rows for every configurable argument and block, named in backticks
  exactly as in the schema, with nested fields as `block.field`. Write-only and trigger arguments (`password_wo`,
  `*_wo_version`, `refresh_revision`) and `timeouts.*` are listed too. A result names the statement kind of an in-place
  change (`ALTER TABLE ... ADD COLUMN`, `GRANT`/`REVOKE`), says "no SQL", or says "replaces the <noun>" with the
  reason, and spells out conditional cases.
- A **Drift.** paragraph: what refresh compares, how a change made outside Terraform shows in the next plan, and what
  is never read back.
- For non-trivial reconciliation (tables, external tables, materialized views, privilege sets), worked examples: a
  short HCL before/after excerpt and the resulting SQL embedded from an existing golden file with
  `{{ codefile "sql" "internal/provider/testdata/sql/<group>/<case>.sql" }}`. Never hand-write SQL that a golden file
  pins.

Derive every row from the code: the schema plan modifiers, the registered replacement policy, the `alterStep`s in
`<type>_sql.go`, Update and Read, and the golden files. `TestReconciliationSectionDocumentsEveryInput` in
`documentation_contract_test.go` requires the section before `## Import`, its table, and every configurable attribute
and block field in backticks, so a new argument fails the tests until it is documented. `task markdown-fmt` does not
format templates, so keep prose within 120 columns and align tables by hand.

SQL spelling follows one style everywhere the provider controls it:

- **Type names** are UPPERCASE (`VARCHAR(64)`, `INTEGER`, `CHARACTER VARYING(256)`) in examples, templates, schema
  descriptions, generated SQL, and every value read back from the catalog. Configuration accepts any case and keeps
  the configured spelling while it is canonically equal.
- **Keyword-like values** are UPPERCASE too: privileges, styles, encodings, `authentication`, identity provider
  `type`, language names, and the `DEFAULT` IAM role. Their validators accept any case.
- **Hand-written SQL in examples** (view queries, defaults, predicates, masking expressions, routine bodies) uses
  UPPERCASE keywords, built-in functions, and casts (`GETDATE()`, `DATEADD(DAY, ...)`, `::VARCHAR`). The provider never
  rewrites SQL that users configure. Terraform functions in HCL (`coalesce`, `lower`, `jsonencode`) stay lowercase.

`TestExampleTypeNamesAreUppercase` checks `type`, `return_type`, and `arguments` values in examples and template
snippets.

`task docs-check` runs the `tfplugindocs` validator against the live provider schema, checking publication layout,
resource/data-source coverage, front matter, and document size limits, and then regenerates the documentation into
`.cache/docs-check` and fails if it differs from `docs/`, including added or missing pages. It is included in
`task check` and CI. Published documentation follows the
[Terraform Registry guidelines](https://developer.hashicorp.com/terraform/registry/providers/docs). Repo-only
`DEVELOPMENT.md` and `TODO.md` live at the repository root; publish end-user guides under `docs/guides/` when needed.

## GitHub Actions

The repository's `.github/workflows/` files run CI and signed releases from the provider repository root.

- `ci.yml` runs on pushes to `main`, pull requests, and manual dispatch. It runs Go lint, `yamllint`, `markdownlint`,
  and `actionlint` (which also applies ShellCheck to `run:` scripts when ShellCheck is installed on the runner), offline
  race tests with coverage, documentation and example checks, and unsigned snapshot builds for Linux/macOS/Windows on
  amd64 and arm64.
- `release.yml` reuses `ci.yml` as a gate and then publishes signed GitHub release assets when a `v*` tag is pushed.
  Only the publishing job has `contents: write`.
- `.github/dependabot.yml` proposes weekly Go module (provider and tools) and GitHub Actions updates.

CI uses only public actions and tools and runs from the repository root, so forks do not need organization workflow
access or secrets for lint, offline tests, documentation checks, or snapshot builds.

CI runs golangci-lint and snapshot builds through their actions and uses `task markdown-fmt-check`, `task test`,
`task terraform-check`, `task terraform-test`, and `task docs-check` for the same checks available locally.
`task terraform-check` builds the local provider once, checks example formatting, then initializes and validates each
directory under `examples/` using the development override and filesystem mirror. The release workflow runs the full CI
workflow before publishing through the version-pinned GoReleaser action with full Git history for the changelog.

## Terraform Registry publication

`.goreleaser.yml` defines platform ZIP archives, SHA256 checksums, a detached GPG checksum signature, and a versioned
manifest declaring Terraform protocol 6. The source address remains `netcheck-de/redshift`.

Before the first signed release:

1. Create `netcheck-de/terraform-provider-redshift` and push this repository to it. Terraform Registry
   publication requires the `terraform-provider-redshift` repository naming convention; the local checkout directory
   can have a different name.
2. [Create and configure the release signing key](#create-and-configure-the-release-signing-key), including the
   repository secrets and Terraform Registry public key.
3. Connect/publish the provider repository in the Terraform Registry namespace.
4. Release `v0.2.0` as described in [Versioning and releases](#versioning-and-releases).

Consumers can then declare `source = "netcheck-de/redshift"` and `version = "~> 0.2"` in `required_providers` and
initialize Terraform normally. `task snapshot` uses GoReleaser v2 to build unsigned archives without publishing. Signed
releases require the GPG key; local checks and snapshots do not require AWS credentials.

### Versioning and releases

The SemVer git tag `vMAJOR.MINOR.PATCH` is the only version source: GoReleaser builds the tagged commit with that
version, and `Taskfile.yml` derives the local build version from the newest tag. Before 1.0, a minor release may break
schemas or behavior and a patch release contains fixes and compatible additions. From 1.0, breaking changes need a
major release with state upgraders or a documented migration.

Release notes are generated from the commit subjects on `main`. Pull requests are squash-merged with the PR title as
the commit subject, so PR titles must follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):
`type(scope): summary` with the types `feat`, `fix`, `docs`, `test`, `ci`, `chore`, `build`, `style`, `refactor`,
`perf`, or `revert`. The scope is optional; `!` after the type or scope marks a breaking change. The "Semantic PR title"
check (`pr.yml`) validates the title. In the repository settings, allow only squash merging with the pull request
title as the default commit message, and require the "Semantic PR title" check in branch protection. GoReleaser groups
the commits since the previous tag into breaking changes, features, fixes, documentation, dependencies (`build(deps)`),
and other changes, and leaves out `test`, `ci`, `chore`, `style`, `refactor`, and other `build` commits. Docs and
examples pin `~> MAJOR.MINOR` and change only when a new release series starts.

To release:

1. Confirm CI on `main` is green; optionally repeat the live `examples/complete` run.
2. For a new series only, update the `~>` constraint in `README.md`, `examples/provider/provider.tf`, and
   `examples/complete/versions.tf`, run `task docs` and `task check`, and commit it as `chore: prepare vX.Y.0`.
3. Tag and push: `git tag -a vX.Y.Z -m "vX.Y.Z"` and `git push origin main vX.Y.Z`.
4. `release.yml` rejects tags that are not `vX.Y.Z` or not on `main`; then it runs CI and publishes the signed release
   with release notes generated from the commits since the previous tag. The Terraform Registry ingests the
   release through its GitHub webhook; confirm the version appears there and that `terraform init` resolves it.
5. Never move or reuse a published tag. Fix a broken release with a new patch version.

### Create and configure the release signing key

The Terraform Registry requires a GPG signature for the release checksum file and verifies it against the public key
registered for the provider's namespace. Use an RSA signing key: the Registry does not accept GnuPG's default ECC key
type. See HashiCorp's
[signing-key instructions](https://developer.hashicorp.com/terraform/registry/providers/publishing#preparing-and-adding-a-signing-key).

1. Install GnuPG. On macOS:

   ```sh
   brew install gnupg
   ```

2. Generate a dedicated, passphrase-protected RSA 4096-bit signing key. Replace the name and email with the release
   owner's identity. GnuPG prompts for the passphrase; retain it for the GitHub repository secret. This example expires
   after two years:

   ```sh
   gpg --quick-generate-key "Your Name <you@example.com>" rsa4096 sign 2y
   ```

3. List the secret keys and copy the new key's full fingerprint, not its short key ID:

   ```sh
   gpg --list-secret-keys --keyid-format long --fingerprint
   ```

4. Export the public and private keys in ASCII-armored format. Substitute the full fingerprint below. The temporary
   directory and restrictive permissions keep the exports outside the repository:

   ```sh
   FINGERPRINT="replace-with-full-fingerprint"
   umask 077
   KEY_DIR=$(mktemp -d)
   gpg --armor --export "$FINGERPRINT" > "$KEY_DIR/signing-public.asc"
   gpg --armor --export-secret-keys "$FINGERPRINT" > "$KEY_DIR/signing-private.asc"
   ```

5. In [Terraform Registry → User Settings → Signing Keys](https://registry.terraform.io/settings/gpg-keys), add the
   complete contents of `signing-public.asc` for the `netcheck-de` organization namespace. This requires organization
   admin access; the key does not need to be added to GitHub's account-level GPG keys.

6. In the `netcheck-de/terraform-provider-redshift` GitHub repository, open **Settings → Secrets and variables →
   Actions** and create these repository secrets:

   - `GPG_PRIVATE_KEY`: the complete contents of `signing-private.asc`, including the `BEGIN` and `END` lines.
   - `PASSPHRASE`: the passphrase entered when generating the key.

   The release workflow imports the private key, obtains its fingerprint, and passes it to GoReleaser as
   `GPG_FINGERPRINT`. No separate fingerprint secret is required.

7. After publishing, download the release's checksum file and detached signature into the same directory and verify
   them. For a `v0.2.0` release:

   ```sh
   gpg --verify terraform-provider-redshift_0.2.0_SHA256SUMS.sig terraform-provider-redshift_0.2.0_SHA256SUMS
   ```

   Confirm that GnuPG reports a good signature from the expected key. On another machine, import `signing-public.asc`
   with `gpg --import` before verifying.

Keep a protected backup of the private key, passphrase, and revocation certificate generated under
`~/.gnupg/openpgp-revocs.d/`. Remove the temporary export directory after securely saving the backup and configuring the
secrets; never commit the private key or passphrase. Before the key expires, renew it with `gpg --edit-key`, re-export
it, and update the Registry public key and GitHub private-key secret. If rotating to a new key instead, register its
public key and update both repository secrets before publishing the next release.
