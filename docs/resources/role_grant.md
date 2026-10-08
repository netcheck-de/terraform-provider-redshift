---
subcategory: Identity and Access
page_title: redshift_role_grant Resource - terraform-provider-redshift
description: Manages a SQL role grant to a user or another role.
---

# redshift_role_grant (Resource)

Grants one role to a receiving role or database user. For example, a group role can inherit `sys:dba`, while Grafana's
database user can inherit `sys:monitor`. See AWS
[GRANT ROLE](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-roles).

## Example Usage

```hcl
resource "redshift_role_grant" "operators" {
  role    = "sys:dba"
  to_role = redshift_role.operators.name

}

resource "redshift_role_grant" "grafana" {
  role    = "sys:monitor"
  to_user = redshift_user.grafana.name
}
```

## Argument Reference

| Name      | Type             | Description                                                                          |
|-----------|------------------|--------------------------------------------------------------------------------------|
| `role`    | String, required | Role being granted, including built-in `sys:` roles. Changing it replaces the grant. |
| `to_role` | String, optional | Receiving role. Specify exactly one of `to_role` and `to_user`.                      |
| `to_user` | String, optional | Receiving database user. Specify exactly one of `to_role` and `to_user`.             |

## Attribute Reference

| Name | Type   | Description                                                       |
|------|--------|-------------------------------------------------------------------|
| `id` | String | Stable JSON identity of the granted role and receiving role/user. |

## Lifecycle and Ownership

Reads check `svv_role_grants` for roles or `svv_user_grants` for users. Deletion revokes only this relationship. A
lifecycle trigger restores the grant after a same-name recipient replacement.

## Import

```sh
terraform import redshift_role_grant.operators \
  '{"workgroup_name":"warehouse","database":"admin","role":"sys:dba","to_role":"ncidc:analytics-operators"}'
```

To import Grafana's role grant, use `"role":"sys:monitor","to_user":"grafana"` in the JSON identity.
