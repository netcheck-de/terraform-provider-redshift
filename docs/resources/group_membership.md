---
subcategory: Identity and Access
page_title: redshift_group_membership Resource - terraform-provider-redshift
description: Manages one user-to-group relationship.
---

# redshift_group_membership (Resource)

Owns one database user's membership in one SQL group. See AWS
[ALTER GROUP](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_GROUP.html).

## Example Usage

```hcl
resource "redshift_group_membership" "reader" {
  group = redshift_group.readers.name
  user  = redshift_user.reader.name

}
```

## Argument Reference

| Name    | Type             | Description                                  |
|---------|------------------|----------------------------------------------|
| `group` | String, required | SQL group name; changes require replacement. |
| `user`  | String, required | SQL user name; changes require replacement.  |

## Attribute Reference

| Name | Type   | Description                                         |
|------|--------|-----------------------------------------------------|
| `id` | String | JSON warehouse/database/group/user import identity. |

## Lifecycle and Ownership

Creation adds this user if absent; deletion removes only this membership. Drift removes a missing relationship from
state so Terraform can restore it. Other group members remain independent.

## Import

```sh
terraform import redshift_group_membership.reader \
  '{"workgroup_name":"warehouse","database":"admin","group":"report_readers","user":"report_reader"}'
```
