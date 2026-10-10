# Complete end-to-end example

Create a disposable provisioned RA3 producer and Redshift Serverless consumer, their networks and IAM roles, native SQL
and S3/Glue sample data, datasharing, users, groups, roles, grants, tables, views, routines, Spectrum external tables,
row-level security, masking, comments, and matching read-only lookups and listings. The default uses one AWS account,
private endpoints, and managed administrator secrets with the Data API. No existing warehouse, VPC, source table, or
Glue catalog is needed.

All thirty-six provider resource types and forty-seven data source types are represented, and every data source is
exposed through an output; `redshift_identity_provider` and its lookup are created only when Identity Center is enabled.
Identity Center is optional: omitting its instance ARN skips the managed SSO application, directory groups/assignments,
and SQL identity provider. The connection configurations are in `providers.tf`, with optional read-only probes in
`connections.tf`.

## Architecture

```text
                         Developer / CI runner
                    Terraform + AWS credentials
                              |
          AWS control-plane APIs + Redshift Data API (HTTPS)
          No laptop-to-VPC SQL connection needed by default
                 |                              |
       +---------v----------------+   +---------v----------------+
       | Producer VPC: 3 AZs      |   | Consumer VPC: 3 AZs      |
       | Private, no NAT          |   | Private, no NAT          |
       |                          |   |                          |
       | Encrypted RA3 cluster    |   | Serverless namespace /   |
       | - admin DB + secret ARN  |   | workgroup                |
       | - owned source database  |   | - admin DB + secret ARN  |
       | - public.fixture rows    |   | - shared DB WITH         |
       | - external schema        |   |   PERMISSIONS            |
       | - outbound datashares    |===| - local DB, schema, view |
       |                          |   | - reader/loader users    |
       | S3 gateway + Glue        |   | - roles / group / grants |
       | interface endpoint       |   |                          |
       +------------+-------------+   | S3 gateway + Glue        |
                    |                 | interface endpoint       |
                    | Spectrum        +------------+-------------+
                    v                              |
       +--------------------------+                | SELECT as reader
       | Owned S3 / Glue fixtures |                v
       | - two-row CSV object     |       Shared data verification
       | - catalog DB + table     |       (Data API, two expected rows)
       +--------------------------+

       === Read-only datasharing, not a VPC TCP link or peering
           Same account: SQL grant to consumer namespace
           Different accounts: account grant + AWS authorization/association

       Optional Identity Center instance ARN
                    |
       Managed Redshift application + directory groups / assignments
                    |
       SQL identity provider + namespaced group roles
                    |
       Ordinary reader/operator roles and their existing grants

       Optional direct SQL: TLS port 5439 from a reachable client
       Private connectivity, or explicit public access + restricted client CIDRs
```

Both VPCs are created by this root. Keeping them separate also supports profiles in different AWS accounts. Datasharing
does not need peering or SQL ingress between the warehouses. Private S3/Glue endpoints support the data fixtures; SSO
additionally creates consumer-side `sso-oauth` and `identitystore` endpoints when enabled.

The setup uses pinned `terraform-aws-modules` modules: `vpc/aws` and its `vpc-endpoints` submodule (`6.6.1`),
`s3-bucket/aws` (`5.14.0`), and `secrets-manager/aws` (`2.1.0`). Each module receives the appropriate producer or consumer
AWS provider. The VPCs use three isolated subnets by default, or three public subnets when public SQL is enabled;
neither mode provisions NAT gateways or assigns public IPs to instances on launch.

Endpoint service names are discovered by the endpoint module through the mapped AWS provider. Bucket policies and SQL
queries reference resource attributes. Glue IAM policies derive catalog/table identifiers from the catalog database ARN
because they must be ready before the warehouse creates the external schema and Glue table.

## Run it

Requirements: Terraform 1.14 or later, an AWS account/credentials with permission to create the listed resources and IAM
roles, a region with RA3/Serverless capacity and at least three available AZs, and the installed Redshift provider. The
default region is `eu-central-1`, the producer is single-node `ra3.xlplus`, and the consumer has fixed base/max capacity
of 8 RPUs. Account quotas and regional availability apply.

Deployment credentials need AWS provisioning and `iam:PassRole` permissions, Data API execute/describe/result access,
and Secrets Manager access to the two managed administrator secrets and the created reader secret. IAM permission and
SQL permission are separate: bootstrap uses the administrator secrets, rather than assuming the caller's IAM-derived
SQL user is a superuser. Shared database lookups also need `redshift:DescribeDataShares`.

For a published provider, from this directory:

```sh
terraform init
terraform apply
terraform output -json warehouses
terraform output -json verification
```

For a provider built from this repository, from the provider root:

```sh
task terraform -- init -backend=false
task terraform -- apply
task terraform -- output -json warehouses
task terraform -- output -json verification
```

No variable file is required for the default. Use the normal AWS credential chain or copy `terraform.tfvars.example`
to `terraform.tfvars` and set `profile`, `region`, or other inputs. The random name suffix isolates AWS resource names.

Apply executes two live checks: a non-superuser reader must see the expected two rows through the shared database,
and the producer must read the two CSV rows through Spectrum. The verification SQL deliberately fails on incorrect
row contents/counts. Each successful query returns `verified = 1`; statement IDs in `verification` can be passed to
`aws redshift-data get-statement-result` using the corresponding account's credentials and region.

This environment incurs charges: the provisioned cluster runs until deleted; Serverless compute/storage, interface
endpoints, S3/Glue storage, and Secrets Manager have their own pricing. Delete it after testing. The example skips final
warehouse snapshots and allows deletion of the owned fixture bucket's contents.

The example has scoped Trivy exceptions for its disposable fixtures: AWS-managed encryption keys (`AWS-0084`,
`AWS-0098`, `AWS-0132`), no VPC flow-log infrastructure (`AWS-0178`), and no S3 access-log bucket or retained versions
(`AWS-0089`, `AWS-0090`). The resources remain encrypted, the bucket blocks public access, SQL uses verified TLS by
default (`direct_sslmode`), and public SQL access requires the explicit restricted-CIDR opt-in. These exceptions are
documented beside the resources; they are not a production security baseline.

## What is managed

| File                       | Responsibility                                                                                       |
|----------------------------|------------------------------------------------------------------------------------------------------|
| `versions.tf`              | Terraform and provider version requirements.                                                         |
| `providers.tf`             | AWS, Random, and all eight Redshift provider configurations (every connection mode).                 |
| `main.tf`                  | Account lookups and shared naming/fixture locals.                                                    |
| `vpc.tf`                   | Two VPC modules, three subnets each, security groups, and private endpoint modules.                  |
| `redshift.tf`              | RA3 cluster and Serverless namespace/workgroup, TLS, IAM attachments, managed passwords.             |
| `iam.tf`                   | Scoped warehouse IAM roles and fixture policies.                                                     |
| `databases.tf`             | Owned SQL databases, local and external schemas, comments, and paired lookups.                       |
| `tables.tf`                | Managed tables with keys, identity, defaults, and AUTO layout, a constraint comment, and a lookup.   |
| `views.tf`                 | Ordinary, late-binding, and materialized views, including a view over a managed table.               |
| `routines.tf`              | SQL function, stored procedure, a function object grant, and paired lookups.                         |
| `extfunctions.tf`          | Lambda UDF with its AWS function and IAM role, and the routine listings.                             |
| `datasharing.tf`           | Datashare membership, grants and privileges, shared database, listings, and paired lookups.          |
| `access_users.tf`          | SQL users, groups, group memberships, and paired lookups.                                            |
| `access_roles.tf`          | SQL roles, role memberships, and paired lookups.                                                     |
| `access_grants.tf`         | Scoped, object, system, ASSUMEROLE, and default privilege grants with paired lookups.                |
| `permissions.tf`           | Column and language grants, including column grants on a managed table, and grant listings.          |
| `rls.tf`                   | Row-level security policy on managed tables, its lookup-table grant, attachment, and table security. |
| `masking.tf`               | Masking policy on managed tables, its lookup-table grant, attachment, and listings.                  |
| `discovery.tf`             | Catalog listings of databases, schemas, tables, columns, and constraints, and a constraint comment.  |
| `s3.tf`                    | Private fixture bucket module, TLS/cross-account policy, and CSV object.                             |
| `spectrum.tf`              | Glue catalog resources/policy, SQL external schema, external table and partition, and lookups.       |
| `secrets.tf`               | Reader credential secret module.                                                                     |
| `bootstrap.tf`             | Idempotent fixture tables/view and optional PUBLIC ASSUMEROLE policy setup.                          |
| `verification.tf`          | Live shared-data and Spectrum queries.                                                               |
| `identity_center.tf`       | Optional SSO application, directory groups/assignments, SQL identity provider and roles.             |
| `connections.tf`           | Optional IAM/password connection probes for both warehouse types.                                    |
| `variables.tf`             | Deployment inputs and validation.                                                                    |
| `outputs*.tf`              | Warehouse/fixture metadata, catalog observations, connection checks, query identities.               |
| `tests/`                   | Mocked `terraform test` suites per feature area and their shared mocks (no AWS access).              |
| `terraform.tfvars.example` | Every input with its default; copy to `terraform.tfvars` and adjust.                                 |

The example exercises each resource's documented variants where a single-account deployment allows it: all comment
targets (database, schema, table, column, view, constraint), object grants to users, roles, groups, and PUBLIC, scoped
grants for every scope including routines and datashare recipients, default privileges per schema and for routines, user
capability flags, and every provider connection mode. Resources also build on each other the way a deployment would: a
view, a column grant, and a constraint comment on a managed table; RLS and masking policies on managed tables that read
managed lookup tables through policy grants; and an object grant on a managed function overload. Not covered by a live
apply: `with_permissions = false` on a second consumer database, a direct-connection `ca_cert_file`, real cross-account
sharing (mocked in `tests/`), and Identity Center without an existing instance.

Administrator passwords are managed by AWS; only their secret ARNs are passed to the SQL provider and exported. The
sample reader's and loader's `random_password` values and the reader secret version **retain the passwords in Terraform
state**, even though `redshift_user.password_wo` is write-only. Use protected state; password values are not exported
by the example.

ASSUMEROLE defaults to enabled. On the newly owned consumer warehouse, initialization revokes unrestricted
`ASSUMEROLE ON ALL FROM PUBLIC FOR ALL` before granting COPY/UNLOAD to the reader role and the attached default IAM
role, and COPY on the explicit consumer IAM role to the loader user. Set `enable_assumerole_grant = false` to omit both
initialization and the managed command grants.

## Optional Identity Center

```hcl
identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-example"
# Optional existing directory user to join the example reader group:
identity_center_test_user_id = "existing-user-id"
# Or map existing directory groups instead of creating example groups (their membership is never changed):
# identity_center_reader_group_name   = "existing-readers"
# identity_center_operator_group_name = "existing-operators"
```

The instance must already exist and be visible in the consumer account/region. For a single-account deployment an
eligible account instance can be used; organization instances support broader account layouts. Terraform creates the
Redshift-managed application and group assignments, but does not enable Identity Center or create a human login
password. If discovery returns multiple identity stores, supply the matching `identity_store_id` explicitly.

The consumer's stable warehouse IAM role receives the optional read-only SSO policy and is reused for the application
and SQL integration. Ordinary `example_readers` and `example_operators` roles stay independent of SSO; additional
namespaced roles use the created directory groups' actual display names and inherit those ordinary roles.

Use an assigned real user and a supported JDBC/browser client to verify SSO, with endpoint connectivity. Creating the
application/groups is configuration coverage, not proof of interactive login. If login creates federated SQL users,
remove those users before teardown; the identity-provider resource refuses to drop an integration with remaining users.

## Connection methods

The default lifecycle uses managed-secret Data API authentication for both warehouse types. Additional probes read the
existing administration database without taking ownership or promoting an IAM-derived SQL identity:

| Probe                      | Warehouse   | Authentication                                              | Network requirement   |
|----------------------------|-------------|-------------------------------------------------------------|-----------------------|
| `producer_data_api_iam`    | Provisioned | Temporary credentials for the configured administrator user | AWS API HTTPS         |
| `consumer_data_api_iam`    | Serverless  | Caller IAM-derived SQL identity                             | AWS API HTTPS         |
| `producer_direct_iam`      | Provisioned | Endpoint discovery and temporary credentials                | SQL endpoint TCP 5439 |
| `consumer_direct_iam`      | Serverless  | Endpoint discovery and temporary credentials                | SQL endpoint TCP 5439 |
| `producer_direct_password` | Provisioned | Ephemeral administrator password                            | SQL endpoint TCP 5439 |
| `consumer_direct_password` | Serverless  | Ephemeral administrator password                            | SQL endpoint TCP 5439 |

```hcl
connection_checks = ["producer_data_api_iam", "consumer_data_api_iam"]
```

IAM probes need the respective `GetClusterCredentials`, `GetWorkgroup`, and `GetCredentials` permissions, plus Data API
permissions for the API modes. Direct connections verify TLS certificates and hostnames. Run them from a client with
private VPC connectivity, or opt in to public SQL with an actual restricted client CIDR:

```hcl
allow_public_sql = true
public_sql_cidrs = ["203.0.113.10/32"] # Replace with your client address.
connection_checks = ["producer_direct_iam", "consumer_direct_iam"]
```

Public mode creates internet gateways/routes and enables public warehouse/share accessibility. It rejects unrestricted
IPv4 ingress and requires a nonempty CIDR list. Account-level public-access restrictions can still prevent creation.

Password probes additionally require ephemeral `TF_VAR_producer_password` / `TF_VAR_consumer_password` values obtained
from the managed secrets outside Terraform state. For example, from this directory with the appropriate account profile:

```sh
export TF_VAR_producer_password="$(aws secretsmanager get-secret-value \
  --secret-id "$(terraform output -json warehouses | jq -r '.producer.admin_secret_arn')" \
  --query SecretString --output text | jq -r '.password')"
```

Repeat for the consumer secret, add the password probe names to `connection_checks`, and run apply from the connected
client. Unset the password variables afterward. The [connection guide](../../docs/guides/connection_methods.md) describes
authentication and identity details.

## Cross-account mode

Set `producer_profile` and `consumer_profile` to profiles for different accounts. Each account gets its own network and
warehouse in the same configured region. The example selects an account-scoped SQL grant, producer AWS authorization,
and consumer namespace association; bucket/catalog policies allow the consumer role to access the owned fixture.

The default same-account path instead grants SQL USAGE directly to the consumer namespace and creates neither AWS
authorization nor association resources. Do not change an existing environment's profiles to migrate it to another
account: use a separate configuration directory/state and a fresh deployment.

The cross-account fixture owns the producer account's Glue catalog resource policy. Use a disposable account without a
pre-existing Glue policy, or integrate that policy deliberately rather than letting this example replace unrelated policy.

## Cleanup and bootstrap semantics

```sh
terraform destroy
# Or, from the provider root:
task terraform -- destroy
```

Keep the deployment's profiles, optional instance ARN, and required ephemeral password inputs available during destroy.
Relationships/grants are removed before their parents, Glue removes its table before restrictive external-schema
deletion, and the owned producer database removes its `public.fixture` table and rows. The AWS-created administration
databases and warehouses are destroyed last. Close interactive SQL sessions that could block database deletion.

`aws_redshiftdata_statement` owns an execution record, not the SQL objects created by it; deleting a statement is a
no-op. Initialization statements are idempotent and contained in the owned disposable database. They do not reconcile
table-definition drift or continuously monitor verification queries, and AWS statement history can expire. Use a fresh
deployment for repeatable tests; if repairing a deliberately removed fixture, replace the initialization statement
resources to reexecute them. There is no populated, independently managed source schema that would block database cleanup.

Use fresh state for this module-based example. It does not include state migrations from earlier complete-example
configurations; create a new state/configuration directory rather than applying it to an existing deployment.

## Configuration tests

The [composition suite](tests/composition.tftest.hcl) uses mocked apply runs for private defaults, ASSUMEROLE disabled,
optional SSO, cross-account sharing, and all connection probes. Each feature area has its own suite next to it, such as
[grants](tests/grants.tftest.hcl), and all suites share the mock values in [tests/mocks](tests/mocks). AWS, Redshift,
and the root Random provider are mocked; these tests require no AWS credentials and create no infrastructure or SQL
objects. The secret module uses a separate
Random alias because Terraform cannot mock ephemeral resource types; its password generation is disabled, so this
alias creates no resources. The bucket module's policy-document merge is mocked with valid JSON, while assertions
check the configured TLS and cross-account policy. Scenario-specific states keep different account and networking
deployments separate.

Run from the provider root:

```sh
task terraform-test
```

CI runs this task and schema validation. Go tests exercise provider implementation; the opt-in acceptance suites and a
real apply exercise Redshift. See [development checks](../../DEVELOPMENT.md#terraform-example-tests).
