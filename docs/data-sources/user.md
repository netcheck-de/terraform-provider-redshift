---
subcategory: Identity and Access
page_title: redshift_user Data Source - terraform-provider-redshift
description: Looks up non-secret attributes of an existing SQL user.
---

# redshift_user (Data Source)

Looks up an existing database user without reading its password.

## Example Usage

```hcl
data "redshift_user" "grafana" {
  name = "grafana"
}
```

## Argument Reference

| Name   | Type             | Description                                                                 |
|--------|------------------|-----------------------------------------------------------------------------|
| `name` | String, required | Database user name, refreshed from the catalog; a missing user is an error. |

## Attribute Reference

`id` (String, computed) is the observed user's JSON identity, using the same warehouse, provider database, and name keys
as the paired resource.

| Name              | Type    | Description                      |
|-------------------|---------|----------------------------------|
| `superuser`       | Boolean | Whether the user has CREATEUSER. |
| `create_database` | Boolean | Whether the user has CREATEDB.   |
