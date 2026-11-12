# Policy grants are not catalog-observable; the live example evaluates the policies as a non-superuser reader.
# These mocked assertions check the wiring and failure-producing SQL, not live SQL execution.
mock_provider "aws" {
  alias  = "producer"
  source = "./tests/mocks/aws_producer"
}
mock_provider "aws" {
  alias  = "consumer"
  source = "./tests/mocks/aws_consumer"
}
mock_provider "random" { source = "./tests/mocks/random" }
mock_provider "redshift" {
  alias  = "producer"
  source = "./tests/mocks/redshift_producer"
}
mock_provider "redshift" {
  alias  = "consumer"
  source = "./tests/mocks/redshift_consumer"
}
mock_provider "redshift" {
  alias  = "producer_data_api_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "consumer_data_api_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "producer_direct_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "consumer_direct_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "producer_direct_password"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "consumer_direct_password"
  source = "./tests/mocks/redshift_connections"
}

run "policy_behavior_verification" {
  command = apply

  assert {
    condition = (
      aws_redshiftdata_statement.verify_rls.secret_arn == module.reader_secret.secret_arn &&
      aws_redshiftdata_statement.verify_masking.secret_arn == module.reader_secret.secret_arn &&
      aws_redshiftdata_statement.verify_rls.secret_arn != aws_redshiftserverless_namespace.consumer.admin_password_secret_arn &&
      aws_redshiftdata_statement.verify_rls.database == redshift_table_security.rls_events.database &&
      aws_redshiftdata_statement.verify_masking.database == redshift_masking_policy_attachment.email_readers.database &&
      strcontains(aws_redshiftdata_statement.verify_rls.sql, "COUNT(*) = 1 AND MIN(id) = 1 AND MIN(region) = 'visible'") &&
      strcontains(aws_redshiftdata_statement.verify_masking.sql, "COUNT(*) = 2") &&
      strcontains(aws_redshiftdata_statement.verify_masking.sql, "id = 1 AND email = '***@example.test'") &&
      strcontains(aws_redshiftdata_statement.verify_masking.sql, "id = 2 AND email = 'allowed@example.test'") &&
      alltrue([for sql in [aws_redshiftdata_statement.verify_rls.sql, aws_redshiftdata_statement.verify_masking.sql] :
        strcontains(sql, "SELECT 1 / CASE") && strcontains(sql, "THEN 1 ELSE 0 END AS verified")
      ]) &&
      output.verification.rls_statement_id == aws_redshiftdata_statement.verify_rls.id &&
      output.verification.masking_statement_id == aws_redshiftdata_statement.verify_masking.id
    )
    error_message = "Policy verification must query as the non-superuser reader and fail when RLS filtering or masking differs."
  }

  assert {
    condition = alltrue([for statement in [
      aws_redshiftdata_statement.rls_regions, aws_redshiftdata_statement.rls_events,
      aws_redshiftdata_statement.masking_exempt, aws_redshiftdata_statement.masking_customers,
      ] : statement.secret_arn == aws_redshiftserverless_namespace.consumer.admin_password_secret_arn &&
      strcontains(statement.sql, "WHERE NOT EXISTS")
    ])
    error_message = "Policy fixtures must be seeded idempotently as the administrator, not the restricted reader."
  }
}

run "rejects_nonlocal_administration_database" {
  command = apply

  override_data {
    target = data.redshift_database.consumer_data_api_iam[0]
    values = { database_type = "SHARED" }
  }

  expect_failures = [data.redshift_database.consumer_data_api_iam]
}
