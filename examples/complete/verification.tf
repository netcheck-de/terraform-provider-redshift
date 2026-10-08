# Query as the non-superuser reader to exercise sharing and role-granted SELECT end to end.
resource "aws_redshiftdata_statement" "verify_shared" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = aws_redshiftserverless_namespace.consumer.db_name
  secret_arn     = module.reader_secret.secret_arn
  # Incorrect or missing rows cause division by zero, so the Data API statement fails the apply.
  sql = "SELECT 1 / CASE WHEN COUNT(*) = 2 AND COUNT(CASE WHEN id = 1 AND label = 'first' THEN 1 END) = 1 AND COUNT(CASE WHEN id = 2 AND label = 'second' THEN 1 END) = 1 THEN 1 ELSE 0 END AS verified FROM ${redshift_database.shared.name}.${redshift_datashare_table.source.schema}.${redshift_datashare_table.source.table}"

  depends_on = [module.reader_secret, redshift_role_grant.reader, redshift_grant.shared_read]
}

resource "aws_redshiftdata_statement" "verify_spectrum" {
  provider           = aws.producer
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  database           = redshift_external_schema.glue.database
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
  sql                = "SELECT 1 / CASE WHEN COUNT(*) = 2 AND COUNT(CASE WHEN id = 1 AND label = 'first' THEN 1 END) = 1 AND COUNT(CASE WHEN id = 2 AND label = 'second' THEN 1 END) = 1 THEN 1 ELSE 0 END AS verified FROM ${redshift_external_schema.glue.name}.${aws_glue_catalog_table.fixture.name}"

  depends_on = [aws_glue_catalog_table.fixture, aws_s3_object.fixture]
}
