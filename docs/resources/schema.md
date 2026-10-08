---
subcategory: Databases and Schemas
page_title: redshift_schema Resource - terraform-provider-redshift
description: Manages a local Redshift database schema.
---

# redshift_schema (Resource)

Manages a local schema in a Redshift database. External schemas and SQLMesh-managed objects have separate ownership. See
AWS [CREATE SCHEMA](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_SCHEMA.html).

## Example Usage

```hcl
resource "redshift_schema" "serving" {
  database = redshift_database.warehouse.name
  name     = "serving"
}
```

## Argument Reference

| Name       | Type             | Description                                                          |
|------------|------------------|----------------------------------------------------------------------|
| `database` | String, required | Local database owning the schema; changing it replaces the resource. |
| `name`     | String, required | Schema name; changing it replaces the resource.                      |

## Attribute Reference

| Name    | Type   | Description                                                          |
|---------|--------|----------------------------------------------------------------------|
| `id`    | String | Stable JSON import identity including workgroup, database, and name. |
| `owner` | String | Database user owning the schema, discovered from the catalog.        |

## Lifecycle and Ownership

The resource creates and drops only the schema. `DROP SCHEMA` does **not** cascade; remove dependent tables, views, and
datashare memberships first. This resource does not grant permissions or manage the schema owner's credentials.

## Import

```sh
terraform import redshift_schema.serving \
  '{"workgroup_name":"warehouse","database":"warehouse","name":"serving"}'
```
