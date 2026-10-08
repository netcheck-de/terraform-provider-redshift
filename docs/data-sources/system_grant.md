---
subcategory: Identity and Access
page_title: redshift_system_grant Data Source - terraform-provider-redshift
description: Reads explicitly granted system capabilities for one SQL role.
---

# redshift_system_grant (Data Source)

Reads explicit SQL system permissions, excluding capabilities inherited from other roles. See AWS
[GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-roles).

## Example Usage

```hcl
data "redshift_system_grant" "operators" {
  role = "operators"
}
```

## Argument Reference

| Name   | Type             | Description        |
|--------|------------------|--------------------|
| `role` | String, required | Existing SQL role. |

## Attribute Reference

`id` (String, computed) is the permission tuple's JSON identity, using the same warehouse, provider database, and role
keys as the paired resource. An existing role with no explicit system privileges still has an ID.

| Name         | Type           | Description                                                 |
|--------------|----------------|-------------------------------------------------------------|
| `privileges` | Set of strings | Explicit SQL capability names; empty when none are granted. |

A missing role raises an error. Read-only observation does not enforce the resource's mutation allowlist or change any
permissions.
