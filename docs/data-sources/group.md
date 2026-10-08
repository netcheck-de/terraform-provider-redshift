---
subcategory: Identity and Access
page_title: redshift_group Data Source - terraform-provider-redshift
description: Looks up an existing SQL user group.
---

# redshift_group (Data Source)

Looks up an existing SQL user group without taking ownership or exposing membership. See AWS
[PG_GROUP](https://docs.aws.amazon.com/redshift/latest/dg/r_PG_GROUP.html).

## Example Usage

```hcl
data "redshift_group" "readers" {
  name = "report_readers"
}
```

## Argument Reference

| Name   | Type             | Description                                           |
|--------|------------------|-------------------------------------------------------|
| `name` | String, required | Existing group name; a missing group raises an error. |

## Attribute Reference

`id` (String, computed) is the observed group's JSON identity, using the same warehouse, provider database, and name keys
as the paired resource.
