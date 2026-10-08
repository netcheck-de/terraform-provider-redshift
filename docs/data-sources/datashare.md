---
subcategory: Data Sharing
page_title: redshift_datashare Data Source - terraform-provider-redshift
description: Looks up an existing outbound datashare.
---

# redshift_datashare (Data Source)

Looks up an existing outbound datashare in its producer database.

## Example Usage

```hcl
data "redshift_datashare" "analytics" {
  database = "warehouse"
  name     = "analytics"
}
```

## Argument Reference

| Name       | Type             | Description                               |
|------------|------------------|-------------------------------------------|
| `name`     | String, required | Datashare name.                           |
| `database` | String, required | Local producer database owning the share. |

## Attribute Reference

`id` (String, computed) is the observed datashare's JSON identity, using the same warehouse, producer database, and name
keys as the paired resource.

| Name                  | Type    | Description                                          |
|-----------------------|---------|------------------------------------------------------|
| `publicly_accessible` | Boolean | Whether public workgroups may consume the datashare. |

A missing share, a share owned by another database, or a share managed by another service is an error.
