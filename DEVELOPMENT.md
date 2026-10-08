# Development and releases

## SQL implementation reference

The AWS [Redshift SQL commands reference](https://docs.aws.amazon.com/redshift/latest/dg/c_SQL_commands.html) is the
source of truth for supported objects, syntax, privileges, and behavior when implementing or extending existing or new
resources. Follow its command-specific documentation and verify catalog and lifecycle behavior against Redshift.

## Local checks

Use Go matching `go.mod`, Terraform 1.14 or later, Task v3, and golangci-lint matching `.golangci-lint-version`. Run
`task check` from the module root to check gofmt formatting, lint, `go vet`, and offline race tests with statement
coverage printed to the terminal. Tests enforce documentation on functions, named types, struct/interface fields, and
package-level settings, including test helpers and doubles, and prevent resource code from importing concrete
transports. [Live acceptance tests](README.md#acceptance-test) are opt-in.

The module-local Taskfile provides `fmt`, `markdown-fmt`, `markdown-fmt-check`, `lint`, `test`, `check`, `build`,
`terraform`, `terraform-check`, `terraform-test`, and `snapshot`. The Terraform runner defaults to `examples/complete`;
override `TF_DIR` for another configuration. Cache and development overrides live in the ignored module-local `.cache/`
directory. No parent Taskfile or platform tooling is required.

## Terraform example tests

The complete example's configuration tests live in `examples/complete/tests/composition.tftest.hcl`. Run them from the
provider root:

```sh
task terraform-test
task terraform-test -- -filter=tests/composition.tftest.hcl -verbose
```

The task builds the local provider, initializes `TF_DIR` without a backend, and runs `terraform test`. The suite mocks
both AWS aliases, all eight Redshift aliases, and Random, so no AWS credentials or warehouse connectivity are needed.
Five runs cover self-provisioning private defaults, ASSUMEROLE disabled, optional SSO, cross-account sharing,
and all connection probes, including their policy and lookup output wiring.

Runs use mocked `apply` operations because Terraform defers dependent lookup reads until apply; deterministic overrides
provide their output values. Terraform manages isolated test state and tears down the mocked objects. The assertions test
the example's HCL composition; Go tests cover provider behavior, and live acceptance tests cover actual Redshift.

The runnable example is `examples/complete`; `examples/connections` contains only a documentation redirect.
The complete README includes its ASCII architecture overview, real apply/destroy instructions, and optional features.

## Lint policy

`task markdown-fmt` formats the root Markdown files and documentation with `go run github.com/bwplotka/mdox@latest`.
`task markdown-fmt-check` checks the same files without rewriting them and runs in `task check` and CI. The formatter
aligns table columns and preserves existing prose line breaks. Markdownlint checks the 120-column prose limit
separately. `@latest` selects the latest tagged mdox release; Go caches the downloaded module and build without adding
dependencies to the provider's `go.mod`.

`.golangci-lint-version` pins the exact golangci-lint release installed by the action in CI and the release workflow.
For local use, install the matching official release binary and put it on `PATH`. `task lint` runs that installed binary
without installing or checking its version. When upgrading the version file, update your local installation too.

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

## GitHub Actions

`task docs-check` runs HashiCorp's pinned `tfplugindocs` validator against the live provider schema, checking
publication layout, resource/data-source coverage, front matter, and document size limits. It is included in
`task check` and CI. Published documentation follows the
[Terraform Registry guidelines](https://developer.hashicorp.com/terraform/registry/providers/docs). Repo-only
`DEVELOPMENT.md` and `TODO.md` live at the repository root; publish end-user guides under `docs/guides/` when needed.

The repository's `.github/workflows/` files run CI and signed releases from the provider repository root.

- `ci.yml` explicitly runs Go lint, `yamllint`, `markdownlint`, `actionlint`, and `shellcheck`, plus offline race tests
  with coverage printed to the terminal, and builds unsigned snapshots for Linux/macOS/Windows on amd64 and arm64.
- `release.yml` runs golangci-lint and publishes signed GitHub release assets when a `v*` tag is pushed.

CI uses only public actions and tools and runs from the repository root, so forks do not need organization workflow
access or secrets for lint, offline tests, documentation checks, or snapshot builds.

CI runs golangci-lint and snapshot builds through their actions and uses `task markdown-fmt-check`, `task test`,
`task terraform-check`, `task terraform-test`, and `task docs-check` for the same checks available locally.
`task terraform-check` builds the local provider once, checks example formatting, then initializes and validates each
directory under `examples/` using the development override and filesystem mirror. The release gate runs the pinned
golangci-lint through its action before publishing through the GoReleaser action.

## Terraform Registry publication

`.goreleaser.yml` defines platform ZIP archives, SHA256 checksums, a detached GPG checksum signature, and a versioned
manifest declaring Terraform protocol 6. The source address remains `netcheck-de/redshift`.

Before the first signed release:

1. Create `netcheck-de/terraform-provider-redshift` and push this standalone checkout to it. Terraform Registry
   publication requires the `terraform-provider-redshift` repository naming convention; the local checkout directory
   can have a different name.
2. [Create and configure the release signing key](#create-and-configure-the-release-signing-key), including the
   repository secrets and Terraform Registry public key.
3. Connect/publish the provider repository in the Terraform Registry namespace.
4. Align the intended version tag, example version constraints, and local Taskfile `VERSION`.
5. Push a semantic version tag such as `v0.1.0`. Registry discovery requires the repository and signing-key setup above.

Consumers can then declare `source = "netcheck-de/redshift"` and `version = "0.1.0"` in `required_providers` and
initialize Terraform normally. `task snapshot` uses GoReleaser v2 to build unsigned archives without publishing. Signed
releases require the GPG key; local checks and snapshots do not require AWS credentials.

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
   them. For a `v0.1.0` release:

   ```sh
   gpg --verify terraform-provider-redshift_0.1.0_SHA256SUMS.sig terraform-provider-redshift_0.1.0_SHA256SUMS
   ```

   Confirm that GnuPG reports a good signature from the expected key. On another machine, import `signing-public.asc`
   with `gpg --import` before verifying.

Keep a protected backup of the private key, passphrase, and revocation certificate generated under
`~/.gnupg/openpgp-revocs.d/`. Remove the temporary export directory after securely saving the backup and configuring the
secrets; never commit the private key or passphrase. Before the key expires, renew it with `gpg --edit-key`, re-export
it, and update the Registry public key and GitHub private-key secret. If rotating to a new key instead, register its
public key and update both repository secrets before publishing the next release.
