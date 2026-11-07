---
subcategory: Upgrade Guides
page_title: Upgrading to version 0.5
description: Breaking changes in version 0.5 and how to update configurations and state.
---

# Upgrading to version 0.5

Version 0.5 changes resource IDs, renames one resource, and gives every resource a listing data source documented
next to it. Some listings that covered several object kinds now list only the kind of their resource. This guide lists
each breaking change and the configuration update it needs.

## Resource and import IDs

IDs no longer contain the connection selector, such as `workgroup_name` or `cluster_identifier`; they hold only the
fields that identify the object, for example `{"database":"warehouse","name":"serving"}` for a schema. Existing state
is normalized on the next refresh, and import blocks that were already applied keep working. New imports must use the
new form shown on each resource page.

## `redshift_default_privileges` is now `redshift_default_privilege`

The resource and its lookup are renamed. `redshift_default_privileges` is now the listing of default privileges.
Rename resource blocks and references, and add a `moved` block per resource so Terraform keeps the existing grants
instead of revoking and recreating them:

```terraform
resource "redshift_default_privilege" "reports" {
  # unchanged arguments
}

moved {
  from = redshift_default_privileges.reports
  to   = redshift_default_privilege.reports
}
```

Rename `data "redshift_default_privileges"` lookups to `data "redshift_default_privilege"`; their arguments are
unchanged.

## Listing data sources

| Data source                  | Change                                                                                                                 | Update                                                                                                                                      |
|------------------------------|------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------|
| `redshift_tables`            | Lists only local tables; `table_type` filter and `table_type` and `remarks` attributes removed                         | Use `redshift_views`, `redshift_materialized_views`, or `redshift_external_tables` for other relations, and `redshift_comments` for remarks |
| `redshift_schemas`           | Lists only local schemas with `quota`; `schema_type` filter and `schema_type` and `source_database` attributes removed | Use `redshift_external_schemas` for external schemas                                                                                        |
| `redshift_functions`         | Lists only SQL functions                                                                                               | Use `redshift_external_functions` for Lambda functions                                                                                      |
| `redshift_procedures`        | `arguments` and `language` removed; `signature` added                                                                  | Read input types from `signature`                                                                                                           |
| `redshift_datashares`        | Lists only outbound datashares; `share_type` filter and inbound-only attributes removed                                | Use `redshift_inbound_datashares` for inbound datashares                                                                                    |
| `redshift_datashare_objects` | Removed                                                                                                                | Use `redshift_datashare_schemas`, `redshift_datashare_tables`, or `redshift_datashare_memberships`                                          |
| `redshift_grants`            | Lists scoped privileges, the grants `redshift_grant` manages                                                           | Use `redshift_object_grants` for the former grants on an object or of a grantee, grouped per object and grantee                             |
| `redshift_column_grants`     | One element per relation and grantee, with a `privileges` map of columns, as `redshift_column_grant` stores them       | Read column privileges from `privileges`                                                                                                    |
