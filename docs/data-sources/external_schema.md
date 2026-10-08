---
subcategory: Databases and Schemas
page_title: redshift_external_schema Data Source - terraform-provider-redshift
description: Looks up a Glue-backed external schema mapping.
---

# redshift_external_schema (Data Source)

Looks up an existing Glue Data Catalog external schema in a local Redshift database. See AWS
[SVV_EXTERNAL_SCHEMAS](https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_EXTERNAL_SCHEMAS.html).

## Example Usage

```hcl
data "redshift_external_schema" "raw" {
  database = "warehouse"
  name     = "raw"
}
```

## Argument Reference

| Name       | Type             | Description                                         |
|------------|------------------|-----------------------------------------------------|
| `database` | String, required | Local Redshift database owning the external schema. |
| `name`     | String, required | External schema name.                               |

## Attribute Reference

`id` (String, computed) is the observed external schema's JSON identity, using the same warehouse, owning database, and
name keys as the paired resource.

| Name            | Type   | Description                                                    |
|-----------------|--------|----------------------------------------------------------------|
| `glue_database` | String | AWS Glue database referenced by the schema.                    |
| `iam_role_arn`  | String | Attached IAM role used for Glue catalog access.                |
| `region`        | String | Glue catalog region recorded in the mapping; null when absent. |

A missing schema, non-Glue schema, or incompatible catalog entry is an error.
