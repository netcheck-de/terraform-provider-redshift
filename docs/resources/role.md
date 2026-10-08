---
subcategory: Identity and Access
page_title: redshift_role Resource - terraform-provider-redshift
description: Manages a Redshift SQL role independently of its grants.
---

# redshift_role (Resource)

Manages one database role, independently of memberships and grants. See AWS
[CREATE ROLE](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_ROLE.html).

## Example Usage

```hcl
resource "redshift_role" "readers" {
  name = "ncidc:analytics-readers"
}
```

## Argument Reference

| Name   | Type             | Description                                   |
|--------|------------------|-----------------------------------------------|
| `name` | String, required | Role name; changing it replaces the resource. |

## Attribute Reference

| Name | Type   | Description                  |
|------|--------|------------------------------|
| `id` | String | Stable JSON import identity. |

## Lifecycle and Ownership

A missing role is planned for creation. Deletion does not cascade; dependent grants and memberships must be removed
first. Referencing the identity provider's namespace orders role creation, but changing that provider under the same
namespace requires explicit replacement of dependent roles.

## Import

```sh
terraform import redshift_role.readers \
  '{"workgroup_name":"warehouse","database":"admin","name":"ncidc:analytics-readers"}'
```
