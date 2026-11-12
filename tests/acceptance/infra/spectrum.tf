locals {
  # Derive the catalog ARN to retain the database's actual partition, region, and account.
  glue_catalog_arn = "${split(":database/", aws_glue_catalog_database.this.arn)[0]}:catalog"
}

# Do not seed tables here: tests create/drop their own catalog entries, and restrictive DROP SCHEMA needs an
# empty catalog after cleanup. Terraform must not co-own those entries with the provider under test.
resource "aws_glue_catalog_database" "this" {
  name        = "${replace(var.name_prefix, "-", "_")}_fixture"
  description = "Disposable catalog for Redshift Spectrum acceptance tests"
}
