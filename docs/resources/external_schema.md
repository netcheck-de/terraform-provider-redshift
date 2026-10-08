---
subcategory: Databases and Schemas
page_title: redshift_external_schema Resource - terraform-provider-redshift
description: Manages a Glue-backed external schema mapping.
---

# redshift_external_schema (Resource)

Manages a Redshift external schema backed by an AWS Glue Data Catalog database. This is a distinct SQL object from a
[local schema](https://registry.terraform.io/providers/netcheck-de/redshift/latest/docs/resources/schema). The namespace
must already have an IAM role that can access Glue and the underlying data. See AWS
[CREATE EXTERNAL SCHEMA](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_EXTERNAL_SCHEMA.html).

## Example Usage

```hcl
resource "redshift_external_schema" "raw" {
  database      = redshift_database.warehouse.name
  name          = "raw"
  glue_database = "raw_catalog"
  iam_role_arn  = aws_iam_role.spectrum.arn
}
```

## Argument Reference

| Name               | Type                      | Description                                                                                                              |
|--------------------|---------------------------|--------------------------------------------------------------------------------------------------------------------------|
| `database`         | String, required          | Local Redshift database owning the external schema; changing it replaces the resource.                                   |
| `name`             | String, required          | External schema name; changing it replaces the resource.                                                                 |
| `glue_database`    | String, required          | AWS Glue database name; changing it replaces the resource.                                                               |
| `iam_role_arn`     | String, required          | Attached IAM role for catalog access; changing it replaces the resource.                                                 |
| `region`           | String, optional/computed | Glue catalog AWS region; defaults to the warehouse region and is read from catalog options. Changes replace the mapping. |
| `refresh_revision` | String, optional          | Bump to recreate the external schema after a catalog change.                                                             |

## Attribute Reference

| Name | Type   | Description                                                                          |
|------|--------|--------------------------------------------------------------------------------------|
| `id` | String | Stable JSON import identity including workgroup, Redshift database, and schema name. |

## Lifecycle and Ownership

Reads `svv_external_schemas`, verifies that it is a Glue Data Catalog schema (`eskind = 1`), and compares its configured
Glue database and IAM role. Creation uses `CREATE EXTERNAL SCHEMA ... FROM DATA CATALOG`. Deletion uses restrictive
`DROP SCHEMA` **without `CASCADE`**. If dependent views exist, remove or migrate them first. Bumping `refresh_revision`
plans replacement; changes in Glue metadata do not automatically appear as Terraform diffs.

## Import

```sh
terraform import redshift_external_schema.raw \
  '{"workgroup_name":"warehouse","database":"warehouse","name":"raw"}'
```

The Glue database and IAM role are discovered on refresh. Match them in configuration before applying.
