---
subcategory: Annotations
page_title: redshift_comment Resource - terraform-provider-redshift
description: Manages existing local object annotations independently of object definitions.
---

# redshift_comment (Resource)

Manages a comment on an existing local database, schema, table, view, or column without owning the object's definition.
See AWS [COMMENT](https://docs.aws.amazon.com/redshift/latest/dg/r_COMMENT.html).

## Example Usage

```hcl
resource "redshift_comment" "schema" {
  database_name = redshift_schema.reporting.database
  object_type   = "SCHEMA"
  object_name   = redshift_schema.reporting.name
  text          = "Curated reporting objects."

}

resource "redshift_comment" "column" {
  database_name = "analytics"
  object_type   = "COLUMN"
  schema_name   = "reporting"
  object_name   = "daily_summary"
  column_name   = "day"
  text          = "UTC reporting date."
}
```

## Argument Reference

| Name            | Type             | Description                                                                   |
|-----------------|------------------|-------------------------------------------------------------------------------|
| `database_name` | String, required | Local database containing the target object.                                  |
| `object_type`   | String, required | `DATABASE`, `SCHEMA`, `TABLE`, `VIEW`, or `COLUMN`.                           |
| `object_name`   | String, required | Database name, schema name, or relation name according to `object_type`.      |
| `schema_name`   | String, optional | Required for `TABLE`, `VIEW`, and `COLUMN`; omit for `DATABASE` and `SCHEMA`. |
| `column_name`   | String, optional | Required only for `COLUMN`; `object_name` identifies its relation.            |
| `text`          | String, required | Desired comment text; an empty string clears the comment using `IS NULL`.     |

For `DATABASE`, `object_name` must equal `database_name`. All identity arguments require replacement; text updates in
place.

## Attribute Reference

| Name | Type   | Description                                                    |
|------|--------|----------------------------------------------------------------|
| `id` | String | Stable JSON administration binding and target-object identity. |

## Lifecycle and Ownership

The SQL caller must own the object or be a superuser. Reads use the target database's `pg_description` and object
catalogs. Database comments execute in the database being annotated, as required by Redshift. Shared database objects,
constraints, external table/column comments, and columns of late-binding views are not supported.

Creation sets the annotation on an existing object; it does not create the object. Read refreshes edited or removed text
so Terraform can repair drift; a missing annotation is represented by an empty `text` string. If the underlying object
disappears, the resource is removed from state. Destroy clears
only the comment and leaves the target intact. It does not restore any annotation overwritten during adoption. Do not
manage the same annotation with another Terraform resource or a model/migration tool. References establish creation order
and changes to target identity inputs require replacement through the provider's schema.

## Import

```sh
terraform import redshift_comment.column \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","object_type":"COLUMN","schema_name":"reporting","object_name":"daily_summary","column_name":"day"}'
```

For database/schema annotations, omit `schema_name` and `column_name`. For table/view annotations, omit `column_name`.
The first refresh reads existing text; imports do not change annotations.
