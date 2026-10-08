---
subcategory: Identity and Access
page_title: redshift_role_grant Data Source - terraform-provider-redshift
description: Checks explicit role-to-role or role-to-user membership.
---

# redshift_role_grant (Data Source)

Checks an explicit role grant, including grants of built-in system roles. Inherited/transitive role access is not
counted. See AWS [GRANT ROLE](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-roles).

## Example Usage

```hcl
data "redshift_role_grant" "reader" {
  role    = "report_readers"
  to_user = "report_reader"
}
```

## Argument Reference

| Name      | Type             | Description                                                 |
|-----------|------------------|-------------------------------------------------------------|
| `role`    | String, required | Granted SQL role.                                           |
| `to_role` | String, optional | Receiving role; exactly one recipient argument is required. |
| `to_user` | String, optional | Receiving SQL user; conflicts with `to_role`.               |

## Attribute Reference

`id` (String, computed) is the relationship's JSON identity, using the same warehouse, provider database, role, and
selected `to_user` or `to_role` keys as the paired resource. It is null when `exists` is false.

| Name     | Type    | Description                                                                    |
|----------|---------|--------------------------------------------------------------------------------|
| `exists` | Boolean | Whether the explicit relationship exists; false also covers absent identities. |

Catalog errors remain errors. No role grant is created, adopted, or revoked.
