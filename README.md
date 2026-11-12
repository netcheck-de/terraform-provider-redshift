# Redshift SQL provider

This Terraform provider manages Redshift Serverless and provisioned SQL objects through the Data API or direct TLS
connections.
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

Terraform 1.16 or later is supported. Versions 1.11–1.15 are untested; versions before 1.11 reject configurations using
write-only arguments such as `password_wo` and `client_secret_wo`.

## Configuration

```hcl
terraform {
  required_providers {
    redshift = {
      source  = "netcheck-de/redshift"
      version = "~> 0.5"
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
IAM, S3/Glue and SQL fixtures, sharing, COPY JOB, and DynamoDB zero-ETL by default. It represents every current
resource/data-source type; an existing Identity Center instance ARN enables optional SSO, and separate account profiles
enable cross-account sharing. Its apply includes blocking readback assertions.
The example incurs AWS charges and needs ordered zero-ETL cleanup; see its README before applying.

The [connection guide](docs/guides/connection_methods.md) covers provisioned Data API, optional secret authentication,
direct password, and direct IAM for both warehouse types. Configure exactly one of `workgroup_name`,
`cluster_identifier`, or `direct_connection`. The [unified example's connection probes](examples/complete/README.md#connection-methods)
show each method against the created warehouses.

The [comment resource](docs/resources/comment.md) annotates existing local objects independently of their definitions.

## Known limitations

- Redshift folds object names to lowercase, quoted ones included, unless `enable_case_sensitive_identifier` is set, so
  the provider rejects database, schema, table, and other object names with uppercase letters at plan time. User names
  and identity-provider principals and roles such as `AAD:alice` or `aad:Sales` keep their case.
- On macOS, native trust can reject Serverless certificates that lack Certificate Transparency timestamps. Add the
  Amazon Trust Services root through `ca_cert_file` to retain full certificate and hostname verification, or connect
  through the Data API. `sslmode = "require"` disables server authentication and is only a disposable-fixture fallback.

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md) for building the provider, local checks, acceptance tests, CI, and releases.

## License

This provider is distributed under the [Mozilla Public License 2.0](LICENSE).
