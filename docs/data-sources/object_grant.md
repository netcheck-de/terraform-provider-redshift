---
subcategory: Identity and Access
page_title: redshift_object_grant Data Source - terraform-provider-redshift
description: Reads explicit permissions on one local object for one SQL identity.
---

# redshift_object_grant (Data Source)

Reads explicit local database/schema/table privileges, including view access through `TABLE`. See AWS
[GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html).

## Example Usage

```hcl
data "redshift_object_grant" "report" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "daily_summary"
  object_type   = "TABLE"
  grantee       = "report_readers"
  grantee_type  = "GROUP"
}
```

## Argument Reference

| Name            | Type             | Description                                             |
|-----------------|------------------|---------------------------------------------------------|
| `database_name` | String, required | Existing local database.                                |
| `object_type`   | String, required | `DATABASE`, `SCHEMA`, or `TABLE`.                       |
| `schema_name`   | String, optional | Required for `SCHEMA` and `TABLE`; omit for `DATABASE`. |
| `object_name`   | String, optional | Required only for `TABLE`; table or view name.          |
| `grantee`       | String, required | Existing SQL identity; use `public` for `PUBLIC`.       |
| `grantee_type`  | String, required | `ROLE`, `USER`, `GROUP`, or `PUBLIC`.                   |

## Attribute Reference

`id` (String, computed) is the permission tuple's JSON identity, using the paired resource's warehouse, provider database,
`database_name`, `schema_name`, `object_name`, `object_type`, `grantee`, and `grantee_type` keys. An existing tuple with
no explicit privileges still has an ID.

| Name         | Type           | Description                                                                                |
|--------------|----------------|--------------------------------------------------------------------------------------------|
| `privileges` | Set of strings | Explicit privilege names for this object/grantee tuple, excluding inherited/scoped grants. |

No explicit grants returns an empty set; a missing target or grantee raises an error. Unlike resource reconciliation,
observational reads can include permissions with grant options or newly introduced privilege names. Grant options
themselves are not returned. No grants are adopted, revoked, or added.
