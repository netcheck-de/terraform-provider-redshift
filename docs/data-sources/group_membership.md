---
subcategory: Identity and Access
page_title: redshift_group_membership Data Source - terraform-provider-redshift
description: Checks one user's explicit SQL group membership without managing it.
---

# redshift_group_membership (Data Source)

Reads explicit SQL user-to-group membership. See AWS
[ALTER GROUP](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_GROUP.html).

## Example Usage

```hcl
data "redshift_group_membership" "reader" {
  group = "report_readers"
  user  = "report_reader"
}
```

## Argument Reference

| Name    | Type             | Description     |
|---------|------------------|-----------------|
| `group` | String, required | SQL group name. |
| `user`  | String, required | SQL user name.  |

## Attribute Reference

`id` (String, computed) is the membership's JSON identity, using the same warehouse, provider database, group, and user
keys as the paired resource. It is null when `exists` is false.

| Name     | Type    | Description                                                                            |
|----------|---------|----------------------------------------------------------------------------------------|
| `exists` | Boolean | True when this explicit membership exists; false when it or either identity is absent. |

The lookup never adds/removes members. SQL/catalog errors are reported, not converted
to `exists = false`.
