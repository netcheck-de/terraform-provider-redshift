# AWS statement resources own execution records, not table lifecycles. Keep fixture DDL idempotent and contained
# in the owned producer database: deleting that database removes its public-schema table and rows.
resource "aws_redshiftdata_statement" "source_table" {
  provider           = aws.producer
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  database           = redshift_database.producer.name
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
  sql                = "CREATE TABLE IF NOT EXISTS ${data.redshift_schema.source.name}.${local.fixture_table_name} (id INTEGER, label VARCHAR(64))"
}

resource "aws_redshiftdata_statement" "source_seed" {
  provider           = aws.producer
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  database           = redshift_database.producer.name
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
  sql                = "INSERT INTO ${data.redshift_schema.source.name}.${local.fixture_table_name} SELECT 1, 'first' WHERE NOT EXISTS (SELECT 1 FROM ${data.redshift_schema.source.name}.${local.fixture_table_name} WHERE id = 1) UNION ALL SELECT 2, 'second' WHERE NOT EXISTS (SELECT 1 FROM ${data.redshift_schema.source.name}.${local.fixture_table_name} WHERE id = 2)"

  depends_on = [aws_redshiftdata_statement.source_table]
}

# This is initialization of a newly owned disposable warehouse, not a per-role resource's hidden policy change.
resource "aws_redshiftdata_statement" "assumerole_policy" {
  provider       = aws.consumer
  count          = var.enable_assumerole_grant ? 1 : 0
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = aws_redshiftserverless_namespace.consumer.db_name
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR ALL"
}
