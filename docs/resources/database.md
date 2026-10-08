---
subcategory: Databases and Schemas
page_title: redshift_database Resource - terraform-provider-redshift
description: Manages a local or datashare-backed Redshift database.
---

# redshift_database (Resource)

Creates a local database, or a consumer database from an **already associated** producer datashare. Datashare
authorization and the namespace-scoped consumer association are AWS resources and must precede shared database creation.
The provider waits up to five minutes for the inbound share to appear in the SQL catalog. See AWS
[CREATE DATABASE](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_DATABASE.html).

## Example Usage

```hcl
resource "redshift_database" "analytics" {
  name          = "analytics"
  datashare_arn = aws_redshift_data_share_consumer_association.analytics.data_share_arn
}

resource "redshift_database" "local" {
  name = "warehouse"
}
```

## Argument Reference

| Name               | Type                       | Description                                                                                         |
|--------------------|----------------------------|-----------------------------------------------------------------------------------------------------|
| `name`             | String, required           | Database name, refreshed from the SQL catalog; changing it replaces the resource.                   |
| `datashare_arn`    | String, optional           | Producer share ARN; omit for a local database. Changing it replaces the resource.                   |
| `with_permissions` | Boolean, optional/computed | Object-level permission mode for shared databases; defaults to `true`. Ignored for local databases. |

## Attribute Reference

| Name                 | Type   | Description                                                             |
|----------------------|--------|-------------------------------------------------------------------------|
| `id`                 | String | Stable JSON import identity, independent of Data API statement history. |
| `database_type`      | String | `local` or `shared`.                                                    |
| `share_name`         | String | Producer share name; null for local databases.                          |
| `producer_account`   | String | Producer AWS account ID; null for local databases.                      |
| `producer_namespace` | String | Producer namespace ID; null for local databases.                        |

## Lifecycle and Ownership

Catalog reads refresh database metadata and, for shared databases, `with_permissions`. The configured `datashare_arn`
is verified against the SQL producer binding; for local databases the ARN is null. Incompatible
existing bindings raise errors. Deletion drops only the matching database, without `CASCADE`; active sessions or grants
may prevent it. This resource manages no `PUBLIC` grants; assign permissions separately.

For local databases, `with_permissions` retains its configured value or default but has no effect; the data source reports
the observed local permission mode as `false`. Resource refreshes do not require AWS datashare discovery permissions.

## Import

```sh
terraform import redshift_database.analytics \
  '{"workgroup_name":"warehouse","database":"admin","name":"analytics","datashare_arn":"arn:aws:redshift:eu-central-1:123456789012:datashare:11111111-2222-3333-4444-555555555555/source"}'
```

For a local database, omit `datashare_arn` from the import ID. For a shared database it must match the existing producer
binding. `with_permissions` is discovered on refresh.
