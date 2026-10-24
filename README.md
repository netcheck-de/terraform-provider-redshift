# Redshift SQL provider

This Go provider manages Redshift Serverless and provisioned SQL objects through the Data API or direct TLS connections.
Use the AWS provider for workgroups, namespaces, IAM, Identity Center applications, and datashare
association/authorization. Its 41 resources and 58 data sources cover:

- databases, including zero-ETL integration destinations, local and external schemas, tables, views, materialized
  views, and Spectrum external tables and partitions;
- SQL and Lambda functions, stored procedures, and Amazon Bedrock external models;
- users, groups, roles, Identity Center or Microsoft Entra ID identity providers, and user lockouts;
- scoped, object (including models and COPY templates), column, language, system, IAM-role, default, and federated
  `DEBUG` privileges;
- row-level security and dynamic data masking;
- COPY templates and automatic COPY jobs;
- datashares with explicit or exclusive schema-kind membership, comments, configuration parameters, and catalog
  listings, including models.

The [resource index](docs/index.md#ownership-boundaries) shows which resource owns which part of a table or datashare.

Terraform 1.16 or later is supported; CI tests the provider and its examples on 1.16 and on the latest release. Older
versions may work for plans and applies but are not tested: their `terraform test` passes optional ephemeral input
variables to the apply phase of a run that left them unset during plan, and rejects the run
([hashicorp/terraform#38974](https://github.com/hashicorp/terraform/issues/38974), fixed in 1.16.0), so the examples'
mocked tests cannot run on them.

## Configuration

```hcl
terraform {
  required_providers {
    redshift = {
      source  = "netcheck-de/redshift"
      version = "~> 0.4"
    }
  }
}

provider "redshift" {
  region         = "eu-central-1"
  profile        = "warehouse-admin"
  workgroup_name = aws_redshiftserverless_workgroup.this.arn
  database       = "admin"
}
```

`region` and `profile` are optional. Without a profile, credentials follow the standard AWS SDK chain; an explicit
profile selects an AWS shared configuration profile, including its assumed-role settings. Use different provider aliases
with separate profiles for cross-account SQL operations. Serverless Data API uses `WorkgroupName` and `Database`; it
does not retrieve an admin password or require a secret ARN. The caller needs Data API permissions and
`redshift-serverless:GetCredentials`, plus the database privileges required by each SQL operation (identity-provider
creation requires a superuser).

The provider selects the warehouse and its default local administration database. Use an alias for another warehouse.
The producer database owning a datashare and the database receiving a grant remain resource arguments because they
identify those SQL objects. Newly created warehouses can be referenced during a fresh apply; imports require known
warehouse selectors, routing, and authentication. Read-only object lookups do not require imports. For resource and
data-source arguments, lifecycle behavior, and imports, see the [resource index](docs/index.md).

Resource arguments are readable attributes, and paired data sources expose the same readable object attributes and JSON
import-compatible `id`. Write-only passwords and Terraform-only rotation/replacement controls are excluded from lookups.
Missing relationship lookups return `exists = false` and a null ID. Shared database lookups expose `datashare_arn` by
calling `redshift:DescribeDataShares`; this requires AWS credentials and a region in the consumer account,
including for direct password SQL connections. Local database lookups do not require AWS metadata discovery.

The [complete example](examples/complete/README.md) provisions a RA3 producer, Serverless consumer, private networks,
IAM, S3/Glue and SQL fixtures, sharing, and every current resource/data-source type. It works in one account by default;
an existing Identity Center instance ARN enables optional SSO, and separate account profiles enable cross-account sharing.

The [connection guide](docs/guides/connection_methods.md) covers provisioned Data API, optional secret authentication,
direct password, and direct IAM for both warehouse types. Configure exactly one of `workgroup_name`,
`cluster_identifier`, or `direct_connection`. The [unified example's connection probes](examples/complete/README.md#connection-methods)
show each method against the created warehouses.

The [provider improvements](TODO.md) track remaining enhancements and acceptance fixtures.

See [development and releases](DEVELOPMENT.md) for local checks, lint policy, CI, and Terraform Registry
publication.

The [comment resource](docs/resources/comment.md) annotates existing local objects independently of their definitions.

## Acceptance test

The opt-in `TestAccLocalSQLLifecycle` runs against an existing test workgroup. The profile must resolve to an IAM user
with privileges to create and drop databases and roles, create schemas, and grant schema privileges. It creates uniquely
named databases, schemas, roles, users, groups, memberships, and object/system/default grants; checks no-change plans
and imports; updates a grant; repairs grant drift; and verifies cleanup:

```sh
TF_ACC=1 \
REDSHIFT_ACC_REGION=eu-central-1 \
REDSHIFT_ACC_PROFILE=test-admin \
REDSHIFT_ACC_WORKGROUP=test-workgroup \
REDSHIFT_ACC_DATABASE=master \
go test ./internal/provider -run '^TestAccLocalSQLLifecycle$' -count=1 -v -timeout=30m
```

Run this command from the provider module directory. Without `TF_ACC=1`, the test is skipped. Cross-account sharing and
Identity Center still require the external fixtures described in the [complete example](examples/complete/README.md).

`TestAccAssumeroleGrantLifecycle` additionally requires `REDSHIFT_ACC_ASSUMEROLE=1`, an attached default IAM role, and a
test warehouse already configured for identity-specific ASSUMEROLE access control. It does not change warehouse-wide
PUBLIC permissions. See the [ASSUMEROLE resource](docs/resources/assumerole_grant.md).

`TestAccCommentLifecycle` uses the same AWS environment variables and a separate disposable database. It verifies
database/schema/table/view/column annotations, imports, text updates, drift repair, and comment-only deletion before
removing its fixtures. Run it with
`go test ./internal/provider -run '^TestAccCommentLifecycle$' -count=1 -v -timeout=30m`.

Direct tests additionally require endpoint connectivity and `REDSHIFT_ACC_DIRECT=1`. `TestAccDirectIAMQueries` verifies
strict TLS and parameter/result handling. `TestAccTransportSwitchLifecycle` verifies stable state across Data API/direct
IAM, imports, updates, and cleanup. `TestAccDirectPasswordPrivileges` verifies a disposable non-superuser password
identity can SELECT its fixture but cannot INSERT. Run each using
`go test ./internal/provider -run '^<test-name>$' -count=1 -v`.
Set `REDSHIFT_ACC_CA_CERT_FILE` to an Amazon Trust Services PEM bundle if macOS rejects the Serverless certificate;
the tests still verify the certificate chain and hostname.

## Known limitations

- Redshift folds object names to lowercase, quoted ones included, unless `enable_case_sensitive_identifier` is set, so
  the provider rejects database, schema, table, and other object names with uppercase letters at plan time. User names
  and identity-provider principals and roles such as `AAD:alice` or `aad:Sales` keep their case.
- On macOS, native trust can reject Serverless certificates that lack Certificate Transparency timestamps. Add the
  Amazon Trust Services root through `ca_cert_file` to retain full certificate and hostname verification, or connect
  through the Data API. `sslmode = "require"` disables server authentication and is only a disposable-fixture fallback.

## License

This provider is distributed under the [Mozilla Public License 2.0](LICENSE).
