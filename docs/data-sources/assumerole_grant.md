---
subcategory: Identity and Access
page_title: redshift_assumerole_grant Data Source - terraform-provider-redshift
description: Reads explicit IAM role command permissions for one SQL identity.
---

# redshift_assumerole_grant (Data Source)

Reads explicit IAM-role usage permissions. See AWS
[GRANT ASSUMEROLE](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-assumerole-permissions).

## Example Usage

```hcl
data "redshift_assumerole_grant" "reader" {
  iam_role_arn = "default"
  grantee      = "report_readers"
  grantee_type = "ROLE"
}
```

## Argument Reference

| Name           | Type             | Description                                       |
|----------------|------------------|---------------------------------------------------|
| `iam_role_arn` | String, required | IAM role ARN, or `default`.                       |
| `grantee`      | String, required | Existing SQL identity; use `public` for `PUBLIC`. |
| `grantee_type` | String, required | `ROLE`, `USER`, `GROUP`, or `PUBLIC`.             |

## Attribute Reference

`id` (String, computed) is the permission tuple's JSON identity, using the same warehouse, provider database,
`iam_role_arn`, `grantee`, and `grantee_type` keys as the paired resource. An existing tuple with no explicit privileges
still has an ID.

| Name         | Type           | Description                                                                                               |
|--------------|----------------|-----------------------------------------------------------------------------------------------------------|
| `privileges` | Set of strings | Explicit command permissions, such as `COPY` and `UNLOAD`; `EXFUNC` is normalized to `EXTERNAL FUNCTION`. |

No matching explicit grants returns an empty set; a missing SQL identity raises an error. Inherited and unrestricted
PUBLIC access are not attributed to individual identities. The lookup does not enable identity-specific access control,
revoke PUBLIC permissions, or require the optional managed ASSUMEROLE grant to be enabled in the complete example.
