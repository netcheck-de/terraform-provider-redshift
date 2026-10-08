---
subcategory: Identity and Access
page_title: redshift_system_grant Resource - terraform-provider-redshift
description: Manages explicit SQL system capabilities for one role.
---

# redshift_system_grant (Resource)

Owns the exact explicit system privilege set for one SQL role. This permits granular capabilities instead of assigning a
complete system role. See AWS [GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-roles).

## Example Usage

```hcl
resource "redshift_system_grant" "operators" {
  role       = redshift_role.operators.name
  privileges = ["CREATE ROLE", "CREATE DATASHARE"]

}
```

## Argument Reference

| Name         | Type                     | Description                                                                |
|--------------|--------------------------|----------------------------------------------------------------------------|
| `role`       | String, required         | Receiving SQL role; changes require replacement.                           |
| `privileges` | Set of strings, required | Exact uppercase system privilege names; empty revokes explicit privileges. |

Supported privileges cover user/schema/table/role/datashare administration; function, external-function, procedure,
view, model and library creation/deletion; `ALTER DEFAULT PRIVILEGES`, `ACCESS CATALOG`, `ACCESS SYSTEM TABLE`,
`TRUNCATE TABLE`, `VACUUM`, `ANALYZE`, `CANCEL`, `IGNORE RLS`, `EXPLAIN RLS`, and `EXPLAIN MASKING`. Use full SQL names
such as `CREATE OR REPLACE FUNCTION`. `ALL` is deliberately not accepted; declare explicit privileges.

## Attribute Reference

| Name | Type   | Description                                   |
|------|--------|-----------------------------------------------|
| `id` | String | JSON warehouse/database/role import identity. |

## Lifecycle and Ownership

Reads `svv_system_privileges`, reconciles extras and missing privileges, and verifies convergence. Inherited permissions
from role memberships are independent. A missing role removes the grant from state. Use custom roles; this resource does
not redefine built-in role capabilities.

Refresh reads the current explicit system privileges into `privileges`.

## Import

```sh
terraform import redshift_system_grant.operators \
  '{"workgroup_name":"warehouse","database":"admin","role":"operators"}'
```
