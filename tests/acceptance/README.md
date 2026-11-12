# Live tests

`.github/workflows/acceptance.yml` runs after successful push CI on `main`.
Releases call the same acceptance workflow, then `.github/workflows/example.yml` to validate the
complete example. Both must succeed before publishing.
Each workflow has its own job using the GitHub **`dev`** environment and independent disposable infrastructure.

## Acceptance tests

Every acceptance run provisions the same fixture: a Serverless warehouse and provisioned producer cluster, S3/Glue and
Parquet data, cross-warehouse sharing, Lambda, and COPY JOB prerequisites. Both warehouses can provision in parallel.
The suite covers core SQL lifecycles, permissions, routines, ASSUMEROLE, IAM/Data API, direct TLS, Spectrum, sharing,
COPY JOB, external functions, and specialized system-setting/lockout probes. There is no basic/full split.
Zero-ETL is provisioned only by the complete example, not by acceptance.
Feature prerequisites control skips; there is no separate hard-coded list of core test names.

Listing regressions cover missing-database behavior across all local-only listings, database-scoped IDs,
SQL-function/procedure parameters, typed grant filters, schema filters, and scoped datashare recipients on merge runs.
The same runs cover shared-database boundaries, actual inbound/consumer listings, and object-grant
listing readback and revocation on the shared table. These checks need no additional services beyond the fixtures below.

General offline CI checks include this fixture, without deploying AWS resources:

```sh
task terraform-check
```

This command checks all Terraform formatting, validates both configurations, and runs this fixture's native mocks with
the pinned released provider. No offline check calls AWS. To test just this fixture, run:

```sh
export TF_CLI_CONFIG_FILE=/dev/null
terraform -chdir=tests/acceptance/infra init -backend=false -input=false
terraform -chdir=tests/acceptance/infra test
```

The workflow lifecycle is:

1. Terraform applies the independent AWS and SQL acceptance fixtures in `infra/`.
2. AWS Data API statement resources create/elevate the assumed role's IAM SQL identity and revoke PUBLIC ASSUMEROLE.
   There are no acceptance preparation scripts or generated AWS profiles.
3. One Go test invocation runs Serverless and cluster acceptance, with each warehouse's own secret.
4. Terraform destroys SQL prerequisites before their dependent AWS infrastructure, including after failures.

The fixture pins Registry provider `netcheck-de/redshift` to **0.4.0** for SQL schemas, tables, sharing and COPY JOB
resources. It never uses the development provider under test and does not deploy or depend on `examples/complete`.
The fixture targets commercial AWS regions (`arn:aws:`), not GovCloud or China partitions.
Fixture commands use `TF_CLI_CONFIG_FILE=/dev/null` so development overrides cannot bypass that pin; offline fixture
tests mock both AWS and the released Redshift provider. Go acceptance tests independently supply the development
provider through the testing framework. Terraform outputs supply `REDSHIFT_ACC_*` identifiers; managed secrets are used
by ARN, without retrieving passwords. Tests and fixture provisioning use the standard AWS credential chain: locally,
`AWS_PROFILE` selects a profile, otherwise the default chain applies; CI uses the OIDC action's environment credentials.
Terraform also exports the actual database, workgroup and region for the tests; CI does not repeat the database default
or maintain its own warehouse-name mapping. `REDSHIFT_ACC_SECRET_ARN` selects the Serverless administrator secret;
`REDSHIFT_ACC_CLUSTER_SECRET_ARN` selects the cluster secret without overriding the Serverless value. Terraform destroy
is always attempted. Provisioning requires
assumed-role credentials, as supplied by CI OIDC, because
Serverless maps that identity to the SQL user `IAMR:<role-name>`. Statement resources record executions, not ownership
of that IAM user or PUBLIC defaults; those bootstrap changes disappear when the disposable warehouse is destroyed.

| Fixture (every acceptance run)                  | Enabled acceptance coverage                                               |
|-------------------------------------------------|---------------------------------------------------------------------------|
| 4-RPU Serverless namespace/workgroup            | SQL lifecycles, IAM/Data API, direct TLS and isolated password users      |
| S3 objects, Glue database, attached IAM role    | Spectrum, Parquet mapping, ASSUMEROLE and result-offload grants           |
| Single-node encrypted `ra3.large` cluster       | Cluster transports, collation and user-lockout listings where supported   |
| Lambda and scoped invocation role               | External-function lifecycle and listings                                  |
| S3 event integration and SQL COPY prerequisites | COPY JOB lifecycle, listings, scope and non-owner visibility              |
| Producer share and consumer database            | Shared grants, routines, concurrent mutations and separate write fixtures |

Routine-grant tests create their own SQL function/table fixtures. Bootstrap revokes the disposable warehouse's default
PUBLIC ASSUMEROLE privileges before testing explicit grants. No SQL fixtures are shared with the complete example.
The non-superuser ordinary-database lookup regression runs in acceptance without a zero-ETL source. Specialized
DynamoDB integration acceptance tests remain available for manually supplied fixtures, but skip in CI. The example
provides end-to-end integration coverage instead; those skips are not counted as executed acceptance coverage.

Remaining exclusions need external inputs: Identity Center, Azure federation, federated debug databases, model-access
roles, Lake Formation accounts, and MySQL zero-ETL. Missing prerequisite flags are reported as skips in verbose Go output.
System-setting and user-lockout probes also skip when the engine reports the feature unavailable. A selected test is not
proof of execution or success; live runtime and engine support still need validation.

## Release example

Before publishing a release:

1. Build the provider from the tagged commit using `task build`.
2. Terraform applies `examples/complete` once using that local provider and its self-contained defaults below.
   Blocking readback postconditions run as part of the apply, just as they do locally.
3. Terraform plans with `-detailed-exitcode`; only **0** (no changes) passes.
4. Delete the DynamoDB zero-ETL integration, wait for deletion, then destroy the example, including after failures.

CI adds an S3 backend file to the example directory with a fixed state key. It does not copy the example, override its
providers, or generate an update-variable file. Features use the example's defaults, not CI-only enable flags:

| Input                             | Default value                                        | Coverage                                                                            |
|-----------------------------------|------------------------------------------------------|-------------------------------------------------------------------------------------|
| `connection_checks`               | `["producer_data_api_iam", "consumer_data_api_iam"]` | Read-only IAM probes on both warehouses                                             |
| `copy_job`                        | `{"auto":false}`                                     | Owned S3 event integration, target table, COPY JOB and lookups; no automatic loads  |
| `zero_etl_create_dynamodb_source` | `true`                                               | Owned on-demand table with PITR/export policy, integration and destination database |

The default `enable_assumerole_grant = true` remains enabled. Warehouses stay private; the IAM probes use HTTPS,
not public SQL ingress. Optional update flags stay at their defaults: this job verifies creation and convergence,
not live in-place updates. Mocked update scenarios and Go acceptance tests provide separate coverage.

### Optional fixture prerequisites and exclusions

Use a region supporting the example's RA3 node type, 4-RPU Serverless capacity, at least three AZs,
[S3 event integrations](https://docs.aws.amazon.com/redshift/latest/dg/loading-data-copy-job.html#loading-data-copy-job-regions),
and [DynamoDB zero-ETL](https://docs.aws.amazon.com/redshift/latest/mgmt/zero-etl-using.regions.html#Aurora-Zero-ETL-DynamoDB).
For example, `eu-central-1` supports these features; account quotas and capacity still apply. CI does not silently skip
features in unsupported regions. Both sources and the target use the same configured account and region.

The example creates the COPY bucket/namespace policies and attached read role. Its bucket has no conflicting existing
object-created notifications. `AUTO OFF` tests job configuration, not ingestion. DynamoDB zero-ETL creates its own table
with PITR and AWS-owned encryption, export policy, namespace policy, and case-sensitive workgroup configuration. No
existing source or account setting is required. The source is empty: this tests integration/database lifecycle, not
replicated row contents or the 15-minute incremental refresh interval.

The deployment role additionally needs Redshift integration create/describe/delete and resource-policy permissions,
inbound-integration authorization, DynamoDB table/PITR/resource-policy management, S3 bucket notification/policy management,
and IAM credential access for both warehouse probes (`redshift:GetClusterCredentials`, `redshift-serverless:GetWorkgroup`,
and `redshift-serverless:GetCredentials`). The example grants scoped export permissions to the Redshift service;
organization policies must allow those operations. CI does not change organization settings or the deployment role.

DynamoDB PITR, exports and Redshift replication can incur charges even for a small fixture. Integration activation and
deletion add asynchronous waits; cleanup waits at most five minutes before reporting failure and attempting the ordinary
destroy. All provisioning, probes, postconditions and cleanup still use the initial one-hour AWS session.
The Aurora alternative (`zero_etl_create_source`) stays disabled: it adds another paid database and about 20 minutes to
apply, leaving less cleanup time. Existing-source zero-ETL is not used.

Identity Center/Azure identity integrations, Bedrock models, cross-account profiles, and Lake Formation grants remain
disabled because they need external account/directory/model-access configuration. The Bedrock example would create
metadata without inference or training, but still requires enabled model access; CI neither requests that access nor
trains paid models. `federated_debug_database` needs federated permissions the example does not register;
`session_context_variable` needs session initialization; `user_lockouts` is unsupported on some warehouses. These remain
unset. Direct IAM/password probes and public SQL stay disabled to avoid extra ingress and password handling; the
acceptance suite covers direct TLS separately.

## GitHub and AWS setup

Configure these **environment variables** under Settings → Environments → **dev**:

| Variable          | Value                        |
|-------------------|------------------------------|
| `AWS_ROLE_ARN`    | OIDC deployment role ARN     |
| `AWS_REGION`      | Test region                  |
| `TF_STATE_BUCKET` | Existing encrypted S3 bucket |

The workflow registers masks for these values before subsequent steps. GitHub variables are not secrets, however:
the runner prints the expanded setup script before those mask commands execute, so this does not guarantee that the
initial setup log hides them. Moving them to job/step `env` has the same limitation. Strict startup-log privacy requires
secret-backed configuration or a separately authorized runtime retrieval mechanism; neither is configured here.

The role needs deployment and deletion permissions for the fixture
and example, access to their managed secrets and Data API, and state/lock access under
`terraform-provider-redshift/live/` (including any required KMS permissions).
Acceptance provisioning additionally needs Redshift cluster/parameter-group and datasharing access, IAM/PassRole,
S3 bucket notification/policy management, Glue, Lambda, and COPY inbound-integration permissions. DynamoDB PITR/export
permissions are needed only for the example.
Both warehouses allow public SQL only from the runner's `/32`; the VPC module owns the routed Redshift subnets and
provisioned-cluster subnet group. There are no NAT gateways. The cluster, Serverless queries, S3 and Lambda can incur
charges on every main-branch acceptance run as well as on releases; DynamoDB PITR/exports are example-only costs.
Provisioning can overlap, but the single Go test invocation does not make individual tests parallel. All tests share
the initial one-hour credentials.

Protect `main` and release-tag creation so only maintainers can initiate deployments. The existing `dev` deployment
rules must allow `main` and `v*` tags. The role's OIDC trust must restrict access to this repository's `dev` environment:

```text
aud: sts.amazonaws.com
sub: repo:netcheck-de/terraform-provider-redshift:environment:dev
```

Pull requests never deploy AWS resources. There is no manual deployment trigger. Release tags must pass the existing
SemVer and main-branch ancestry guard. No organization IAM or environment policy changes are made by these workflows.

## State and cleanup

Acceptance uses one stable resource-name prefix and encrypted S3 state key. The example stays separate:

```text
redshift-ci-acceptance -> terraform-provider-redshift/live/acceptance.tfstate
redshift-ci-complete   -> terraform-provider-redshift/live/complete.tfstate
```

Acceptance uses the fixed `redshift-dev-acceptance` concurrency group; the example uses
`redshift-dev-complete`. Running jobs are not cancelled. The release dependency still runs acceptance before the example.
A later run reuses the suite's state, including resources left by a failed cleanup; recovery can still require manual
intervention. Older per-run state keys, or the former `acceptance-full.tfstate` key, if previously deployed, need separate
cleanup with their original inputs; changing this workflow does not migrate those resources.

Jobs use GitHub's default execution limit, one initial one-hour AWS session and no credential refresh steps. Go's test
timeout is disabled (`-timeout=0`). If provisioning/tests exceed the credential lifetime, AWS calls and cleanup can fail;
cancellation or runner loss can also interrupt cleanup. Use the same commit, configuration, inputs and state key to
run `terraform destroy` with test-account credentials. State can contain passwords; do not publish it or upload it as
a workflow artifact. There is no automatic orphan cleanup.

For the complete suite, first delete any remaining `aws_redshift_integration.zero_etl[0]` with
`aws redshift delete-integration --integration-arn <ARN from state>` and wait until it disappears from
`aws redshift describe-integrations`. Terraform cannot drop the destination database while that integration exists.
The workflow reads its ARN from state (including after partial applies), not just the completed example's outputs;
the subsequent destroy refreshes away the deleted integration. Keep the original configuration and all enabled inputs
when recovering cleanup.

For acceptance recovery, use fresh assumed-role credentials, `TF_CLI_CONFIG_FILE=/dev/null`,
`TF_VAR_name_prefix=redshift-ci-acceptance`, and the `acceptance.tfstate` key.
Run `terraform destroy`; the pinned provider removes the owned SQL fixtures before Terraform removes the warehouses.
SQL objects created by tests have their own cleanup; leftovers that block restrictive SQL deletion require inspection.
Keep the failed run's configuration during recovery; provider connections depend on those warehouses, so mixing SQL
teardown with warehouse reconfiguration can introduce dependency cycles.
Do not remove a state lock while a deployment may still be active.

If an older shell-managed acceptance fixture was deployed, clean it up using that original commit before applying this
configuration. Its SQL objects were not tracked in Terraform, and its zero-ETL resources require the original ordered
cleanup. This refactor does not automatically import or delete those pre-existing objects.
