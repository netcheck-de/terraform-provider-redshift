---
subcategory: Data Sharing
page_title: redshift_datashare_table Resource - terraform-provider-redshift
description: Manages explicit table or view membership in a datashare.
---

# redshift_datashare_table (Resource)

Manages one existing table or view membership in a producer datashare schema. See AWS
[ALTER DATASHARE](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DATASHARE.html).

## Example Usage

```hcl
resource "redshift_datashare_table" "view" {
  database  = redshift_datashare_schema.serving.database
  datashare = redshift_datashare_schema.serving.datashare
  schema    = redshift_datashare_schema.serving.schema
  table     = "example_view"
}
```

## Argument Reference

| Name        | Type             | Description                                                            |
|-------------|------------------|------------------------------------------------------------------------|
| `database`  | String, required | Local producer database; changing it replaces membership.              |
| `datashare` | String, required | Producer datashare name; changing it replaces membership.              |
| `schema`    | String, required | Schema already included in the share; changing it replaces membership. |
| `table`     | String, required | Existing table or view; changing it replaces membership.               |

## Attribute Reference

| Name | Type   | Description                                          |
|------|--------|------------------------------------------------------|
| `id` | String | Stable JSON identity for this table/view membership. |

## Lifecycle and Ownership

Uses `ALTER DATASHARE ... ADD/REMOVE TABLE` and reads `svv_datashare_objects` for tables, views, late-binding views, and
materialized views. It does not create or drop the underlying table/view. Add the schema to the share first; remove
member tables/views before removing the shared schema. New objects need separate membership resources.

## Import

```sh
terraform import redshift_datashare_table.view \
  '{"workgroup_name":"warehouse","database":"warehouse","datashare":"analytics","schema":"serving","table":"example_view"}'
```
