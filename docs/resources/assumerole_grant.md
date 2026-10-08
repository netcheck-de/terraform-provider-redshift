---
subcategory: Identity and Access
page_title: redshift_assumerole_grant Resource - terraform-provider-redshift
description: Manages IAM role command permissions for one SQL identity.
---

# redshift_assumerole_grant (Resource)

Owns the exact command set for one IAM role and one SQL identity. See AWS
[GRANT ASSUMEROLE](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-assumerole-permissions).

## Example Usage

```hcl
resource "redshift_assumerole_grant" "loader" {
  iam_role_arn = "arn:aws:iam::123456789012:role/redshift-data"
  grantee      = redshift_role.loader.name
  grantee_type = "ROLE"
  privileges   = ["COPY", "UNLOAD"]

}
```

## Argument Reference

| Name           | Type                     | Description                                                                        |
|----------------|--------------------------|------------------------------------------------------------------------------------|
| `iam_role_arn` | String, required         | IAM role ARN, or `default` for the namespace default IAM role.                     |
| `grantee`      | String, required         | SQL identity name; use `public` for `PUBLIC`.                                      |
| `grantee_type` | String, required         | `ROLE`, `USER`, `GROUP`, or `PUBLIC`.                                              |
| `privileges`   | Set of strings, required | `COPY`, `UNLOAD`, `EXTERNAL FUNCTION`, `CREATE MODEL`; empty revokes owned grants. |

Identity changes require replacement. Declare commands explicitly; `ALL` is not accepted.

## Attribute Reference

| Name | Type   | Description                                               |
|------|--------|-----------------------------------------------------------|
| `id` | String | JSON warehouse/database/IAM-role/grantee import identity. |

## Lifecycle and Ownership

Reads `svv_iam_privileges` and reconciles only the selected IAM role and SQL identity. AWS owns IAM policies, trust, and
namespace role attachments. The warehouse must already have identity-specific ASSUMEROLE access control enabled:
Redshift rejects individual grants while unrestricted `ASSUMEROLE ON ALL TO PUBLIC FOR ALL` remains active. The resource
does not change that warehouse-wide policy. The opt-in `TestAccAssumeroleGrantLifecycle` requires
`REDSHIFT_ACC_ASSUMEROLE=1` in addition to the normal acceptance-test environment and an attached default IAM role.

Refresh reads the current explicit command permissions into `privileges`.

## Import

```sh
terraform import redshift_assumerole_grant.loader \
  '{"workgroup_name":"warehouse","database":"admin","iam_role_arn":"default","grantee":"loader","grantee_type":"ROLE"}'
```
