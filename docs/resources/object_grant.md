---
subcategory: Identity and Access
page_title: redshift_object_grant Resource - terraform-provider-redshift
description: Manages explicit privileges on one local database object for one SQL grantee.
---

# redshift_object_grant (Resource)

Owns the exact explicit privilege set for one local database object and one grantee. Tables and views use `TABLE`. See
AWS [GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html).

## Example Usage

```hcl
resource "redshift_object_grant" "report" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "daily_summary"
  object_type   = "TABLE"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["SELECT"]

}
```

## Argument Reference

| Name            | Type                     | Description                                                       |
|-----------------|--------------------------|-------------------------------------------------------------------|
| `database_name` | String, required         | Existing local database.                                          |
| `object_type`   | String, required         | `DATABASE`, `SCHEMA`, or `TABLE`.                                 |
| `schema_name`   | String, optional         | Required for `SCHEMA` and `TABLE`; omit for `DATABASE`.           |
| `object_name`   | String, optional         | Required only for `TABLE`; table or view name.                    |
| `grantee`       | String, required         | Identity name; use `public` for `PUBLIC`.                         |
| `grantee_type`  | String, required         | `ROLE`, `USER`, `GROUP`, or `PUBLIC`.                             |
| `privileges`    | Set of strings, required | Exact uppercase privilege set; an empty set revokes owned grants. |

All identity arguments require replacement when changed; `privileges` updates in place. Database privileges: `CREATE`,
`USAGE`, `TEMPORARY`, `ALTER`. Schema privileges: `CREATE`, `USAGE`, `ALTER`, `DROP`. Table privileges: `SELECT`,
`INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`, `ALTER`, `TRUNCATE`. Redshift validates which privileges apply to
the specific object, including external schemas.

## Attribute Reference

| Name | Type   | Description                                                                    |
|------|--------|--------------------------------------------------------------------------------|
| `id` | String | JSON identity containing the administration binding and object/grantee fields. |

## Lifecycle and Ownership

The resource checks object/grantee existence and reads `SHOW GRANTS ON` the object in the local database. Scoped,
inherited, and other grantee grants are independent. It revokes extras, adds missing privileges, and verifies
convergence. Grant options are not managed; existing grant-option rows raise an error before mutation. Missing parents
remove the resource from state. Avoid overlapping this resource with `redshift_grant` for the same explicit
schema/database tuple. Function/procedure signatures, column grants, shared database objects, and Lake Formation IAM
grants are future work.

Refresh reads the current explicit grants into `privileges`.

## Import

```sh
terraform import redshift_object_grant.report \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","schema_name":"reporting","object_name":"daily_summary","object_type":"TABLE","grantee":"report_readers","grantee_type":"GROUP"}'
```
