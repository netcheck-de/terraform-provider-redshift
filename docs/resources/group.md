---
subcategory: Identity and Access
page_title: redshift_group Resource - terraform-provider-redshift
description: Manages a SQL user group independently of its membership and privileges.
---

# redshift_group (Resource)

Manages a SQL user group. Groups contain database users; they are distinct from Identity Center group roles. See AWS
[CREATE GROUP](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_GROUP.html) and
[DROP GROUP](https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_GROUP.html).

## Example Usage

```hcl
resource "redshift_group" "readers" {
  name = "report_readers"
}
```

## Argument Reference

| Name   | Type             | Description                                  |
|--------|------------------|----------------------------------------------|
| `name` | String, required | SQL group name; changes require replacement. |

## Attribute Reference

| Name | Type   | Description                                    |
|------|--------|------------------------------------------------|
| `id` | String | JSON warehouse/database/group import identity. |

## Lifecycle and Ownership

Creation requires a SQL superuser. Memberships and privileges are separate resources; this resource does not reconcile
them. Delete does not remove users or revoke unrelated grants. Redshift refuses deletion while the group has object
privileges. Order managed grants and memberships through references; group-name changes propagate into their immutable
identity inputs and require replacement through the provider's schema.

## Import

```sh
terraform import redshift_group.readers \
  '{"workgroup_name":"warehouse","database":"admin","name":"report_readers"}'
```
