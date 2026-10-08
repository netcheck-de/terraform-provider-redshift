---
subcategory: Databases and Schemas
page_title: redshift_database Data Source - terraform-provider-redshift
description: Looks up an existing local or shared database.
---

# redshift_database (Data Source)

Looks up an existing local or shared database without managing it.

## Example Usage

```hcl
data "redshift_database" "analytics" {
  name = "analytics"
}
```

## Argument Reference

| Name   | Type             | Description          |
|--------|------------------|----------------------|
| `name` | String, required | Database to look up. |

## Attribute Reference

| Name                 | Type    | Description                                               |
|----------------------|---------|-----------------------------------------------------------|
| `id`                 | String  | JSON identity matching the paired resource's import ID.   |
| `datashare_arn`      | String  | Backing producer datashare ARN; null for local databases. |
| `database_type`      | String  | `local` or `shared`.                                      |
| `with_permissions`   | Boolean | Whether the shared database requires object-level grants. |
| `share_name`         | String  | Producer share name; null for local databases.            |
| `producer_account`   | String  | Producer AWS account ID; null for local databases.        |
| `producer_namespace` | String  | Producer namespace ID; null for local databases.          |

Missing or ambiguous databases and shared databases without a complete producer binding are errors.

Shared-database lookups discover the complete producer ARN through
`redshift:DescribeDataSharesForConsumer` in the consumer account. The caller needs that permission and AWS credentials
and a region, including when the SQL connection uses a password. The returned ARN retains the producer's region and
partition; it is not constructed from the consumer's region. Missing or ambiguous AWS metadata is an error.
Local database lookups require no AWS metadata calls and return `with_permissions = false`.
