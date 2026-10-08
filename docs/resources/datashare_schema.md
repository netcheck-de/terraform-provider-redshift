---
subcategory: Data Sharing
page_title: redshift_datashare_schema Resource - terraform-provider-redshift
description: Manages schema membership and future-object inclusion in a datashare.
---

# redshift_datashare_schema (Resource)

Manages one schema membership in a producer datashare. Schema creation and table/view membership are separate resources.
See AWS [ALTER DATASHARE](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DATASHARE.html).

## Example Usage

```hcl
resource "redshift_datashare_schema" "serving" {
  database  = redshift_datashare.producer.database
  datashare = redshift_datashare.producer.name
  schema    = redshift_schema.serving.name
  include_new = true
}
```

## Argument Reference

| Name          | Type                       | Description                                                                         |
|---------------|----------------------------|-------------------------------------------------------------------------------------|
| `database`    | String, required           | Local producer database; changing it replaces the membership.                       |
| `datashare`   | String, required           | Producer datashare name; changing it replaces the membership.                       |
| `schema`      | String, required           | Existing schema; changing it replaces the membership.                               |
| `include_new` | Boolean, optional/computed | Add future objects in this schema automatically; default `false`, updated in place. |

## Attribute Reference

| Name | Type   | Description                                      |
|------|--------|--------------------------------------------------|
| `id` | String | Stable JSON identity for this schema membership. |

## Lifecycle and Ownership

Uses `ALTER DATASHARE ... ADD/REMOVE SCHEMA` and reads `svv_datashare_objects`. It owns this relationship only. Remove
shared tables/views before removing their schema membership. Deleting the membership does not drop the producer schema.
Refresh updates `include_new` from the observed future-object inclusion policy.
Reference managed datashare and schema names to establish creation order. Changes to those names require membership
replacement through the provider's schema.
`include_new` does not add **existing** tables or views; include them with separate `redshift_datashare_table`
resources. The existing producer provisioner uses a blanket `GRANT SELECT FOR TABLES IN SCHEMA` policy. Its exact
catalog/drift semantics remain a live migration check; `include_new` alone does not replace existing object membership.

## Import

```sh
terraform import redshift_datashare_schema.serving \
  '{"workgroup_name":"warehouse","database":"warehouse","datashare":"analytics","schema":"serving"}'
```
