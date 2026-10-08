---
subcategory: Data Sharing
page_title: redshift_datashare Resource - terraform-provider-redshift
description: Manages an outbound Redshift datashare.
---

# redshift_datashare (Resource)

Manages **one producer-side SQL datashare** in the local database specified by `database`. This is a different SQL
object from the
[consumer database](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/database). AWS
datashare authorization and consumer association are managed with the AWS provider. See AWS
[CREATE DATASHARE](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_DATASHARE.html).

## Example Usage

```hcl
resource "redshift_datashare" "analytics" {
  database = redshift_database.warehouse.name
  name     = "analytics"
}
```

## Argument Reference

| Name                  | Type                       | Description                                                                          |
|-----------------------|----------------------------|--------------------------------------------------------------------------------------|
| `database`            | String, required           | Local producer database owning the datashare; changing it replaces the resource.     |
| `name`                | String, required           | Datashare name, unique in the producer namespace; changing it replaces the resource. |
| `publicly_accessible` | Boolean, optional/computed | Whether public workgroups may consume the share, default `false`; updated in place.  |

## Attribute Reference

| Name | Type   | Description                            |
|------|--------|----------------------------------------|
| `id` | String | Stable JSON identity of the datashare. |

## Lifecycle and Ownership

Reads check that the share is outbound, belongs to the configured database, and is not managed by another service.
Creation and updates verify the catalog state. Deletion uses `DROP DATASHARE` without cascading into the producer
database. Schema/table membership and account usage grants are managed by separate provider resources.

## Import

```sh
terraform import redshift_datashare.analytics \
  '{"workgroup_name":"warehouse","database":"warehouse","name":"analytics"}'
```

The producer database in the import ID must match the datashare's actual source database. Public accessibility is
discovered on refresh.
