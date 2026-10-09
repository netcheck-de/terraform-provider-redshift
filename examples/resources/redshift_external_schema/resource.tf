resource "redshift_external_schema" "raw" {
  database      = redshift_database.warehouse.name
  name          = "raw"
  glue_database = "raw_catalog"
  iam_role_arn  = aws_iam_role.spectrum.arn
}
