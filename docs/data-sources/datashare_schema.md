---
subcategory: Data Sharing
page_title: redshift_datashare_schema Data Source - terraform-provider-redshift
description: Reads schema membership and future-object inclusion in a producer datashare.
---

# redshift_datashare_schema (Data Source)

Reads explicit schema membership and its actual future-object sharing policy. See AWS
[ALTER DATASHARE](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DATASHARE.html).

## Example Usage

```hcl
data "redshift_datashare_schema" "reporting" {
  database  = "analytics"
  datashare = "reports"
  schema    = "reporting"
}
```

## Argument Reference

| Name        | Type             | Description                 |
|-------------|------------------|-----------------------------|
| `database`  | String, required | Existing producer database. |
| `datashare` | String, required | Outbound SQL share name.    |
| `schema`    | String, required | Producer schema name.       |

## Attribute Reference

`id` (String, computed) is the relationship's JSON identity, using the same warehouse, producer database, datashare, and
schema keys as the paired resource. It is null when `exists` is false.

| Name          | Type    | Description                                                                              |
|---------------|---------|------------------------------------------------------------------------------------------|
| `exists`      | Boolean | Whether the schema is explicitly a share member.                                         |
| `include_new` | Boolean | Whether future schema objects are automatically shared; false when membership is absent. |

An absent share/schema membership returns `exists = false`. Missing databases, SQL errors, and malformed catalog rows
raise errors. This lookup does not add schemas, include existing tables, or change `include_new`.
