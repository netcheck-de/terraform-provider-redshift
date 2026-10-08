---
subcategory: Databases and Schemas
page_title: redshift_schema Data Source - terraform-provider-redshift
description: Looks up a local schema and its SQL owner.
---

# redshift_schema (Data Source)

Looks up an existing local schema without managing it.

## Example Usage

```hcl
data "redshift_schema" "serving" {
  database = "warehouse"
  name     = "serving"
}
```

## Argument Reference

| Name       | Type             | Description                           |
|------------|------------------|---------------------------------------|
| `database` | String, required | Local database containing the schema. |
| `name`     | String, required | Schema name.                          |

## Attribute Reference

`id` (String, computed) is the observed schema's JSON identity, using the same warehouse, owning database, and name keys
as the paired resource.

| Name    | Type   | Description                      |
|---------|--------|----------------------------------|
| `owner` | String | Database user owning the schema. |

A missing schema or incomplete catalog ownership is an error.
