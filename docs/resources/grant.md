---
subcategory: Identity and Access
page_title: redshift_grant Resource - terraform-provider-redshift
description: Manages role or producer datashare privileges within a database or schema scope.
---

# redshift_grant (Resource)

Manages the exact privilege set for **one role or datashare, database, and scope**. Other recipients, scopes, and
object-specific grants are independent. An empty `privileges` set revokes the privileges in this tuple. See AWS
[GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html).

## Example Usage

```hcl
resource "redshift_grant" "readers" {
  for_each = {
    DATABASE = ["USAGE"]
    SCHEMAS  = ["USAGE"]
    TABLES   = ["SELECT"]
  }

  database_name = redshift_database.analytics.name
  role          = redshift_role.readers.name
  scope         = each.key
  privileges    = each.value

}
```

## Argument Reference

| Name            | Type                     | Description                                                                                                    |
|-----------------|--------------------------|----------------------------------------------------------------------------------------------------------------|
| `database_name` | String, required         | Local or shared database; changing it replaces the grant.                                                      |
| `schema_name`   | String, optional         | Required for `SCHEMA`; optional with `TABLES` for a schema-scoped table grant. Changing it replaces the grant. |
| `role`          | String, optional         | Receiving role; exactly one of role or datashare is required. Changing it replaces the grant.                  |
| `datashare`     | String, optional         | Producer share; requires a local database, schema_name, and SCHEMA or TABLES scope.                            |
| `scope`         | String, required         | `DATABASE`, `SCHEMAS`, `SCHEMA`, `TABLES`, `FUNCTIONS`, or `PROCEDURES`; changing it replaces the grant.       |
| `privileges`    | Set of strings, required | Exact uppercase SQL privileges for this tuple; an empty set revokes them.                                      |

Supported values are `USAGE`, `CREATE`, `TEMPORARY`, `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`, and
`TRUNCATE`, `ALTER`, and `EXECUTE`. Redshift still determines which privileges are valid for each scope. An empty set
revokes all privileges within the owned tuple.

## Attribute Reference

| Name | Type   | Description                                                              |
|------|--------|--------------------------------------------------------------------------|
| `id` | String | Stable JSON identity for the database, optional schema, role, and scope. |

## Lifecycle and Ownership

The resource reads `SHOW GRANTS FOR ROLE` in the target database for local databases and in the administration database
for shared databases. It revokes unexpected privileges in the owned tuple and grants missing ones. An unsupported
existing privilege raises an error before mutation. `DATABASE` `USAGE` is for shared databases; local databases receive
`SCHEMAS` `USAGE` and `TABLES` `SELECT`. The provider does not manage individual users, `PUBLIC` grants, object-specific
grants, or direct system privileges. Use
[object grants](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/object_grant) for
explicit local object/user/group privileges and
[system grants](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/system_grant) for
role capabilities.

Use `scope = "SCHEMA"`, `schema_name = redshift_schema.serving.name`, and `privileges = ["USAGE"]` for explicit schema
access. With `scope = "TABLES"` and `schema_name` set, the grant applies to all current and future tables in that
schema. Scope and privilege combinations are ultimately validated by Redshift.

Use `FUNCTIONS` or `PROCEDURES` with `EXECUTE` for scoped routine execution, optionally limited by `schema_name`.
Redshift scoped routine permissions do not distinguish between functions and procedures. Declare one such tuple per
role/database/schema to avoid overlapping ownership; prefer `FUNCTIONS` consistently.

Reference managed role and database names to establish creation order. Changes to those names propagate into the grant's
immutable inputs and Terraform replaces the grant using the provider's schema policy.

## Import

For a producer datashare, use `datashare` instead of `role`. `SCHEMA` supports `USAGE`; `TABLES` supports `SELECT`. Both
require `schema_name`. Reads use `SHOW GRANTS ON SCHEMA`, filtering the catalog recipient `ds:<share>`. Scoped table
permissions cover existing and future tables and late-binding views, including view replacement. Do not overlap these
tuples with explicit datashare schema/table memberships.

```hcl
resource "redshift_grant" "share_tables" {
  database_name = "analytics"
  schema_name   = "serving"
  datashare     = "analytics_share"
  scope         = "TABLES"
  privileges    = ["SELECT"]
  depends_on    = [redshift_grant.share_schema]
}
resource "redshift_grant" "share_schema" {
  database_name = "analytics"
  schema_name   = "serving"
  datashare     = "analytics_share"
  scope         = "SCHEMA"
  privileges    = ["USAGE"]
}
```

The datashare import identity uses `datashare` in place of `role` and includes `schema_name`:

```sh
terraform import redshift_grant.share_tables \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","datashare":"analytics_share","schema_name":"serving","scope":"TABLES"}'
```

```sh
terraform import 'redshift_grant.readers["TABLES"]' \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","role":"ncidc:analytics-readers","scope":"TABLES"}'
```

The actual privilege set is read from the catalog during refresh; compare the next plan to the intended policy.
