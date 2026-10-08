---
subcategory: Identity and Access
page_title: redshift_grant Data Source - terraform-provider-redshift
description: Reads explicit role permissions within one database or schema scope.
---

# redshift_grant (Data Source)

Reads an explicit scoped privilege set, excluding inherited role access and other scopes. See AWS
[GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-scoped-syntax).

## Example Usage

```hcl
data "redshift_grant" "readers" {
  database_name = "analytics"
  role          = "report_readers"
  scope         = "TABLES"
  schema_name   = "reporting"
}
```

## Argument Reference

| Name            | Type             | Description                                                                     |
|-----------------|------------------|---------------------------------------------------------------------------------|
| `database_name` | String, required | Existing local or shared database.                                              |
| `role`          | String, optional | Existing SQL role; exactly one of role or datashare is required.                |
| `datashare`     | String, optional | Producer share, with a local database, schema_name, and SCHEMA or TABLES scope. |
| `scope`         | String, required | `DATABASE`, `SCHEMAS`, `SCHEMA`, `TABLES`, `FUNCTIONS`, or `PROCEDURES`.        |
| `schema_name`   | String, optional | Required for `SCHEMA`; optional for `TABLES`, `FUNCTIONS`, and `PROCEDURES`.    |

## Attribute Reference

`id` (String, computed) is the permission tuple's JSON identity in the paired resource format. It includes the warehouse,
provider database, `database_name`, `scope`, and selected `role` or `datashare`; `schema_name` is included when configured.
An existing tuple with no explicit privileges still has an ID.

| Name         | Type           | Description                                                                      |
|--------------|----------------|----------------------------------------------------------------------------------|
| `privileges` | Set of strings | Current explicit privileges for this tuple; empty when no matching grants exist. |

A missing database, role, or datashare raises an error. This data source does not reconcile privilege sets.
Local grants read in the target database; shared grants read through the administration database.
