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

# SHOW MODEL omits the default response type on some engines. Check every generated inference overload's actual
# catalog return type instead of inferring VARCHAR from a missing setting; this does not invoke Bedrock.
resource "aws_redshiftdata_statement" "verify_model_response" {
  provider       = aws.consumer
  count          = local.bedrock_enabled ? 1 : 0
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_external_model.summarizer[0].database
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "SELECT 1 / CASE WHEN COUNT(*) > 0 AND COUNT(CASE WHEN format_type(p.prorettype, NULL) = 'character varying' THEN 1 END) = COUNT(*) THEN 1 ELSE 0 END AS verified_model_response FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = '${redshift_external_model.summarizer[0].schema}' AND p.proname = '${redshift_external_model.summarizer[0].function}'"
}

# Policy SELECT grants have no catalog readback. Seed disposable owned tables, then evaluate each policy as the reader
# rather than claiming that an attachment's existence proves the policy can access its lookup table.
resource "aws_redshiftdata_statement" "rls_regions" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_table.rls_regions.database
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "INSERT INTO ${redshift_table.rls_regions.schema}.${redshift_table.rls_regions.name} (reader, region) SELECT '${redshift_user.reader.name}', 'visible' WHERE NOT EXISTS (SELECT 1 FROM ${redshift_table.rls_regions.schema}.${redshift_table.rls_regions.name} WHERE reader = '${redshift_user.reader.name}' AND region = 'visible')"
}

resource "aws_redshiftdata_statement" "rls_events" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_table.rls_events.database
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "INSERT INTO ${redshift_table.rls_events.schema}.${redshift_table.rls_events.name} (id, region) SELECT fixture.id, fixture.region FROM (SELECT 1 AS id, 'visible' AS region UNION ALL SELECT 2, 'hidden') fixture WHERE NOT EXISTS (SELECT 1 FROM ${redshift_table.rls_events.schema}.${redshift_table.rls_events.name} existing WHERE existing.id = fixture.id)"
}

resource "aws_redshiftdata_statement" "verify_rls" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_table_security.rls_events.database
  secret_arn     = module.reader_secret.secret_arn
  sql            = "SELECT 1 / CASE WHEN COUNT(*) = 1 AND MIN(id) = 1 AND MIN(region) = 'visible' THEN 1 ELSE 0 END AS verified FROM ${redshift_table_security.rls_events.schema}.${redshift_table_security.rls_events.relation}"

  # Table names do not express seeded-row, secret-version, or reader-permission dependencies.
  depends_on = [
    aws_redshiftdata_statement.rls_regions, aws_redshiftdata_statement.rls_events, module.reader_secret,
    redshift_role_grant.reader, redshift_grant.local_schema, redshift_grant.local_schema_tables,
  ]
}

resource "aws_redshiftdata_statement" "masking_exempt" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_table.masking_exempt.database
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "INSERT INTO ${redshift_table.masking_exempt.schema}.${redshift_table.masking_exempt.name} (email) SELECT 'allowed@example.test' WHERE NOT EXISTS (SELECT 1 FROM ${redshift_table.masking_exempt.schema}.${redshift_table.masking_exempt.name} WHERE email = 'allowed@example.test')"
}

resource "aws_redshiftdata_statement" "masking_customers" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_table.customers.database
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
  sql            = "INSERT INTO ${redshift_table.customers.schema}.${redshift_table.customers.name} (id, email) SELECT fixture.id, fixture.email FROM (SELECT 1 AS id, 'hidden@example.test' AS email UNION ALL SELECT 2, 'allowed@example.test') fixture WHERE NOT EXISTS (SELECT 1 FROM ${redshift_table.customers.schema}.${redshift_table.customers.name} existing WHERE existing.id = fixture.id)"
}

resource "aws_redshiftdata_statement" "verify_masking" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  database       = redshift_masking_policy_attachment.email_readers.database
  secret_arn     = module.reader_secret.secret_arn
  sql            = "SELECT 1 / CASE WHEN COUNT(*) = 2 AND COUNT(CASE WHEN id = 1 AND email = '***@example.test' THEN 1 END) = 1 AND COUNT(CASE WHEN id = 2 AND email = 'allowed@example.test' THEN 1 END) = 1 THEN 1 ELSE 0 END AS verified FROM ${redshift_masking_policy_attachment.email_readers.schema}.${redshift_masking_policy_attachment.email_readers.relation}"

  # The policy attachment orders its lookup grant, but not fixture rows, secret versions, or reader access.
  depends_on = [
    aws_redshiftdata_statement.masking_exempt, aws_redshiftdata_statement.masking_customers, module.reader_secret,
    redshift_role_grant.reader, redshift_grant.local_schema, redshift_grant.local_schema_tables,
  ]
}
