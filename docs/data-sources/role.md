---
subcategory: Identity and Access
page_title: redshift_role Data Source - terraform-provider-redshift
description: Looks up an existing SQL role.
---

# redshift_role (Data Source)

Looks up an existing database role without taking ownership of it.

## Example Usage

```hcl
data "redshift_role" "readers" {
  name = "ncidc:analytics-readers"
}
```

## Argument Reference

| Name   | Type             | Description                                                        |
|--------|------------------|--------------------------------------------------------------------|
| `name` | String, required | Role name, refreshed from the catalog; a missing role is an error. |

## Attribute Reference

`id` (String, computed) is the observed role's JSON identity, using the same warehouse, provider database, and name keys
as the paired resource.
