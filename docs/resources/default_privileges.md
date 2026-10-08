---
subcategory: Identity and Access
page_title: redshift_default_privileges Resource - terraform-provider-redshift
description: Manages explicit default permissions for future objects created by one SQL user.
---

# redshift_default_privileges (Resource)

Owns default privileges for future objects created by one user in one local database, optionally restricted to a schema.
See AWS [ALTER DEFAULT PRIVILEGES](https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DEFAULT_PRIVILEGES.html).

## Example Usage

```hcl
resource "redshift_default_privileges" "reports" {
  database_name = "analytics"
  owner         = redshift_user.loader.name
  schema_name   = "reporting"
  object_type   = "TABLES"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["SELECT"]

}
```

## Argument Reference

| Name            | Type                     | Description                                                                |
|-----------------|--------------------------|----------------------------------------------------------------------------|
| `database_name` | String, required         | Existing local database.                                                   |
| `owner`         | String, required         | User creating future objects; explicit rather than the executing identity. |
| `schema_name`   | String, optional         | Existing schema; omit for database-wide defaults.                          |
| `object_type`   | String, required         | `TABLES`, `FUNCTIONS`, or `PROCEDURES`.                                    |
| `grantee`       | String, required         | Identity name; use `public` for `PUBLIC`.                                  |
| `grantee_type`  | String, required         | `ROLE`, `USER`, `GROUP`, or `PUBLIC`.                                      |
| `privileges`    | Set of strings, required | Exact explicit default privilege set; empty revokes owned grants.          |

All identity arguments require replacement. Tables support `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`,
and `TRUNCATE`; functions/procedures support `EXECUTE`.

## Attribute Reference

| Name | Type   | Description                                                                  |
|------|--------|------------------------------------------------------------------------------|
| `id` | String | JSON administration binding and creator/schema/object-type/grantee identity. |

## Lifecycle and Ownership

Reads `svv_default_privileges` in the target database. Changing another user's defaults requires appropriate SQL
administration privileges. Schema-specific defaults add to database-wide defaults; this resource owns only its exact
tuple. Existing tables/views/functions/procedures are unaffected, including on destroy. Grant options are not managed
and cause an error before mutation. Scoped permissions are different: they cover current and future objects regardless
of creator. Built-in implicit PUBLIC privileges are not part of the explicit set owned by this resource.

Refresh reads the current explicit defaults into `privileges`.

## Import

```sh
terraform import redshift_default_privileges.reports \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","owner":"loader","schema_name":"reporting","object_type":"TABLES","grantee":"report_readers","grantee_type":"GROUP"}'
```
