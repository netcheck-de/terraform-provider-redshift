---
subcategory: Data Sharing
page_title: redshift_datashare_table Data Source - terraform-provider-redshift
description: Checks explicit table or view membership in a producer datashare.
---

# redshift_datashare_table (Data Source)

Checks catalog membership of a producer table or view. See AWS
[ALTER DATASHARE](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DATASHARE.html).

## Example Usage

```hcl
data "redshift_datashare_table" "report" {
  database  = "analytics"
  datashare = "reports"
  schema    = "reporting"
  table     = "daily_summary"
}
```

## Argument Reference

| Name        | Type             | Description                              |
|-------------|------------------|------------------------------------------|
| `database`  | String, required | Existing producer database.              |
| `datashare` | String, required | Outbound share name.                     |
| `schema`    | String, required | Producer schema containing the relation. |
| `table`     | String, required | Table or view name.                      |

## Attribute Reference

`id` (String, computed) is the relationship's JSON identity, using the same warehouse, producer database, datashare,
schema, and table keys as the paired resource. It is null when `exists` is false.

| Name     | Type    | Description                                                      |
|----------|---------|------------------------------------------------------------------|
| `exists` | Boolean | Whether the selected relation is a catalog member of this share. |

Missing memberships return false; missing databases and catalog errors remain errors. The lookup never modifies the
source relation or share.
