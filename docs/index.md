---
page_title: redshift Provider
description: Manages Redshift Serverless and provisioned SQL objects through Data API or direct TLS connections.
---

# redshift Provider

Manage Redshift SQL objects and permissions through Data API or direct TLS connections. Use the AWS provider for
workgroups, namespaces, IAM roles, Identity Center applications, and datashare authorization/association. The SQL
provider does not retrieve administration passwords automatically; direct connections require endpoint connectivity. See
the AWS [SQL commands reference](https://docs.aws.amazon.com/redshift/latest/dg/c_SQL_commands.html).

## Example Usage

```hcl
terraform {
  required_providers {
    redshift = {
      source  = "netcheck-de/redshift"
      version = "0.2.0"
    }
  }
}

provider "redshift" {
  region         = "eu-central-1"
  profile        = "warehouse-admin"
  workgroup_name = "analytics"
  database       = "master"
}
```

## Argument Reference

| Name                 | Type             | Description                                                                                  |
|----------------------|------------------|----------------------------------------------------------------------------------------------|
| `workgroup_name`     | String, optional | Serverless Data API name/ARN; select exactly one warehouse method.                           |
| `database`           | String, required | Existing local administration database for catalog queries.                                  |
| `region`             | String, optional | AWS Region; defaults to AWS SDK configuration.                                               |
| `profile`            | String, optional | AWS shared configuration profile; otherwise use the standard SDK credential chain.           |
| `cluster_identifier` | String, optional | Provisioned Data API cluster name; conflicts with workgroup_name/direct_connection.          |
| `db_user`            | String, optional | Existing SQL user for provisioned Data API temporary credentials; conflicts with secret_arn. |
| `secret_arn`         | String, optional | Data API Secrets Manager credentials; conflicts with db_user/direct_connection.              |
| `direct_connection`  | Block, optional  | Direct TLS connection using password or nested IAM configuration.                            |

`direct_connection` accepts optional `host`, `port`, `username`, sensitive `password`, `ca_cert_file`, and a nested
`iam` block. Password mode requires host/username/password. IAM requires exactly one workgroup_name or
cluster_identifier; cluster IAM also requires an existing db_user. Port defaults to 5439 or the discovered port. See
[connection methods](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/guides/connection_methods)
for complete configuration, permissions, TLS, and warehouse binding details.

Profiles may include assumed-role settings. Use provider aliases with separate profiles for additional accounts or
warehouses. The caller needs Data API permissions, `redshift-serverless:GetCredentials`, and the SQL privileges required
by each operation. Identity-provider and SQL group creation require a SQL superuser.

Catalog mutations are serialized within each configured provider instance, across its databases, to avoid Redshift
catalog transaction conflicts. Read-only SELECT and SHOW statements remain concurrent. Separate provider instances or
external SQL clients are not coordinated by this gate.

## Reading arguments and attributes

Argument Reference documents configurable inputs, including optional/computed arguments. Except for write-only
arguments, these values are also readable through Terraform references. Attribute Reference lists only additional
computed-only outputs; arguments are not repeated there.

## Resources

The provider currently implements nineteen SQL resources and nineteen read-only data sources. AWS infrastructure remains
with the `hashicorp/aws` provider.

| Resource                                                                                                                               | Description                               |
|----------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------|
| [`redshift_database`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/database)                     | Local or consumer database                |
| [`redshift_datashare`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/datashare)                   | Producer datashare                        |
| [`redshift_identity_provider`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/identity_provider)   | AWSIDC SQL identity provider              |
| [`redshift_role`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/role)                             | Redshift database role                    |
| [`redshift_user`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/user)                             | Database user                             |
| [`redshift_schema`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/schema)                         | Local database schema                     |
| [`redshift_external_schema`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/external_schema)       | Glue external schema                      |
| [`redshift_datashare_schema`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/datashare_schema)     | Producer share schema member              |
| [`redshift_datashare_table`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/datashare_table)       | Producer share table member               |
| [`redshift_datashare_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/datashare_grant)       | SQL account or namespace share grant      |
| [`redshift_role_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/role_grant)                 | `GRANT ROLE` membership                   |
| [`redshift_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/grant)                           | Scoped `GRANT` privileges                 |
| [`redshift_group`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/group)                           | SQL user group                            |
| [`redshift_group_membership`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/group_membership)     | User-to-group membership                  |
| [`redshift_object_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/object_grant)             | Explicit local object privileges          |
| [`redshift_system_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/system_grant)             | Role system privileges                    |
| [`redshift_assumerole_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/assumerole_grant)     | IAM role command permissions              |
| [`redshift_default_privileges`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/default_privileges) | Creator-specific future object privileges |
| [`redshift_comment`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/comment)                       | Existing object annotations               |

## Data sources

| Data source                                                                                                                                  | Lookup                            |
|----------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------|
| [`data.redshift_database`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/database)                   | Existing local or shared database |
| [`data.redshift_datashare`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/datashare)                 | Existing outbound datashare       |
| [`data.redshift_identity_provider`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/identity_provider) | Existing AWSIDC provider          |
| [`data.redshift_role`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/role)                           | Existing role                     |
| [`data.redshift_group`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/group)                         | Existing SQL user group           |
| [`data.redshift_user`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/user)                           | Existing user (no password)       |
| [`data.redshift_schema`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/schema)                       | Existing local schema             |
| [`data.redshift_external_schema`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/external_schema)     | Existing Glue external schema     |

Relationship data sources return `exists`; datashare schema lookups additionally return the observed `include_new`
policy. Permission data sources return explicit `privileges` without adopting or reconciling them; no matching grants
produce an empty set, while missing permission parents raise errors. Comment lookups return `text`, using an empty
string for an unannotated existing object. All data sources expose readable object attributes matching their paired
resource, including a computed JSON `id` compatible with the resource's import identity. Lookup identifiers remain
required or optional inputs; observed settings are computed. Write-only passwords, password-rotation counters, and
replacement triggers are resource-only controls. Data sources never execute mutation SQL and do not take ownership;
missing relationships return `exists = false` with `id = null`.

Shared database lookups additionally require `redshift:DescribeDataSharesForConsumer` in the consumer account to resolve
the backing `datashare_arn`. AWS credentials and a region are needed for that metadata read even with a direct password
SQL connection. Local database lookups and shared database resource refreshes do not make this AWS metadata call.

| Data source                                                                                                                                    | Lookup                                     |
|------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------|
| [`data.redshift_group_membership`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/group_membership)     | Explicit group membership                  |
| [`data.redshift_role_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/role_grant)                 | Explicit role relationship                 |
| [`data.redshift_datashare_schema`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/datashare_schema)     | Schema membership and future-object policy |
| [`data.redshift_datashare_table`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/datashare_table)       | Explicit relation membership               |
| [`data.redshift_datashare_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/datashare_grant)       | SQL account usage                          |
| [`data.redshift_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/grant)                           | Explicit scoped privileges                 |
| [`data.redshift_object_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/object_grant)             | Explicit object privileges                 |
| [`data.redshift_system_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/system_grant)             | Role system capabilities                   |
| [`data.redshift_assumerole_grant`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/assumerole_grant)     | IAM role command permissions               |
| [`data.redshift_default_privileges`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/default_privileges) | Explicit creator-specific defaults         |
| [`data.redshift_comment`](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/data-sources/comment)                       | Existing annotation text                   |

## Provider connection and imports

The provider selects one warehouse and a local administration database. Serverless Data API configuration remains valid:

```hcl
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = aws_redshiftserverless_workgroup.this.arn
  database       = "admin"
}
```

`workgroup_name` accepts a Serverless workgroup name or ARN. `profile` optionally selects an AWS shared configuration
profile for that alias; otherwise the AWS SDK credential chain applies. Use provider aliases for additional warehouses.
The datashare resource requires its producer database separately; grants require the database receiving privileges.
Scoped grants connect to that database for local grants and the administration database for shared grants.

All resources export a computed `id` containing their stable import identity as JSON; it is independent of Data API
statement IDs. Import IDs contain exactly one of `workgroup_name`, `cluster_identifier`, or `endpoint`, the relevant
`database`, and the resource-specific keys documented on each page. The first refresh reads the existing object; import
does not execute creation SQL. Configure the provider with a known workgroup before importing. A resource rejects state
whose stored workgroup or database differs from its provider binding, rather than adopting a same-named object in
another warehouse. Check the import plan before applying changes.

Object names and immutable bindings require replacement. For a warehouse migration, explicitly rebind/import resources
under the new provider alias; changing provider configuration does not automatically move SQL objects. Reads refresh
mutable catalog state and remove missing resources from state so Terraform can recreate them. Writes check convergence;
deletion uses no `CASCADE`. Resource references order dependent operations, and changes to immutable referenced inputs
require replacement through the provider's schema. Same-name recreation can require a follow-up refresh/plan/apply to
reconcile relationships lost with the parent object.
