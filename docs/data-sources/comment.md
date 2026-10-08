---
subcategory: Annotations
page_title: redshift_comment Data Source - terraform-provider-redshift
description: Reads a local object's annotation without adopting or clearing it.
---

# redshift_comment (Data Source)

Reads a local database, schema, table, view, or column annotation. See AWS
[COMMENT](https://docs.aws.amazon.com/redshift/latest/dg/r_COMMENT.html).

## Example Usage

```hcl
data "redshift_comment" "column" {
  database_name = "analytics"
  object_type   = "COLUMN"
  object_name   = "daily_summary"
  schema_name   = "reporting"
  column_name   = "day"
}
```

## Argument Reference

| Name            | Type             | Description                                                               |
|-----------------|------------------|---------------------------------------------------------------------------|
| `database_name` | String, required | Existing local database containing the annotated object.                  |
| `object_type`   | String, required | `DATABASE`, `SCHEMA`, `TABLE`, `VIEW`, or `COLUMN`.                       |
| `object_name`   | String, required | Database/schema/relation name; must equal `database_name` for `DATABASE`. |
| `schema_name`   | String, optional | Required for `TABLE`, `VIEW`, `COLUMN`; omit for `DATABASE`, `SCHEMA`.    |
| `column_name`   | String, optional | Required only for `COLUMN`.                                               |

## Attribute Reference

`id` (String, computed) is the object's JSON identity in the paired resource format. It includes the warehouse, provider
database, `database_name`, `object_type`, and `object_name`; `schema_name` and `column_name` are included when configured.
An existing object with no annotation still has an ID.

| Name   | Type   | Description                                                                 |
|--------|--------|-----------------------------------------------------------------------------|
| `text` | String | Current annotation; an unannotated existing object returns an empty string. |

A missing object raises an error. Shared objects, constraints, external relation comments, and late-binding view column
comments are unsupported. The lookup never creates, changes, or clears an annotation.
