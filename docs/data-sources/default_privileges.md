---
subcategory: Identity and Access
page_title: redshift_default_privileges Data Source - terraform-provider-redshift
description: Reads explicit creator-specific default permissions for future objects.
---

# redshift_default_privileges (Data Source)

Reads one explicit default-privilege tuple without changing permissions on current or future objects. See AWS
[ALTER DEFAULT PRIVILEGES](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DEFAULT_PRIVILEGES.html).

## Example Usage

```hcl
data "redshift_default_privileges" "reports" {
  database_name = "analytics"
  owner         = "report_loader"
  schema_name   = "reporting"
  object_type   = "TABLES"
  grantee       = "report_readers"
  grantee_type  = "GROUP"
}
```

## Argument Reference

| Name            | Type             | Description                                       |
|-----------------|------------------|---------------------------------------------------|
| `database_name` | String, required | Existing local database.                          |
| `owner`         | String, required | Existing SQL user creating future objects.        |
| `schema_name`   | String, optional | Existing schema; omit for database-wide defaults. |
| `object_type`   | String, required | `TABLES`, `FUNCTIONS`, or `PROCEDURES`.           |
| `grantee`       | String, required | Existing SQL identity; use `public` for `PUBLIC`. |
| `grantee_type`  | String, required | `ROLE`, `USER`, `GROUP`, or `PUBLIC`.             |

## Attribute Reference

`id` (String, computed) is the permission tuple's JSON identity, using the paired resource's warehouse, provider database,
`database_name`, `owner`, `object_type`, `grantee`, and `grantee_type` keys. `schema_name` is included when configured.
An existing tuple with no explicit privileges still has an ID.

| Name         | Type           | Description                                                                  |
|--------------|----------------|------------------------------------------------------------------------------|
| `privileges` | Set of strings | Current explicit defaults for this exact tuple; empty when no entries exist. |

Missing parents raise errors. Global and schema-specific defaults are distinct; implicit PUBLIC defaults and inherited
access are excluded. Grant-option rows can be observed, but their grant-option flags are not returned.
