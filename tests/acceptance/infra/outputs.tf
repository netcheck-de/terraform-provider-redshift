output "acceptance" {
  description = "Environment inputs for the acceptance suites; ARNs only, never secret values."
  sensitive   = true
  value = {
    REDSHIFT_ACC_REGION                     = var.region
    REDSHIFT_ACC_DATABASE                   = aws_redshiftserverless_namespace.this.db_name
    REDSHIFT_ACC_WORKGROUP                  = aws_redshiftserverless_workgroup.this.workgroup_name
    REDSHIFT_ACC_SECRET_ARN                 = aws_redshiftserverless_namespace.this.admin_password_secret_arn
    REDSHIFT_ACC_SPECTRUM_ROLE_ARN          = aws_iam_role.this.arn
    REDSHIFT_ACC_SPECTRUM_GLUE_DATABASE     = aws_glue_catalog_database.this.name
    REDSHIFT_ACC_SPECTRUM_LOCATION          = local.spectrum_location
    REDSHIFT_ACC_PARQUET_LOCATION           = local.parquet_location
    REDSHIFT_ACC_ASSUMEROLE                 = "1"
    REDSHIFT_ACC_CLUSTER                    = aws_redshift_cluster.producer.cluster_identifier
    REDSHIFT_ACC_CLUSTER_SECRET_ARN         = aws_redshift_cluster.producer.master_password_secret_arn
    REDSHIFT_ACC_DB_USER                    = aws_redshift_cluster.producer.master_username
    REDSHIFT_ACC_LAMBDA_FUNCTION            = aws_lambda_function.extfunctions_upper.function_name
    REDSHIFT_ACC_LAMBDA_ROLE                = aws_iam_role.lambda_invocation.arn
    REDSHIFT_ACC_COPY_SOURCE                = local.copy_source
    REDSHIFT_ACC_COPY_ROLE                  = aws_iam_role.this.arn
    REDSHIFT_ACC_COPY_JOB_DATABASE          = redshift_copy_job.this.database
    REDSHIFT_ACC_COPY_JOB_NAME              = redshift_copy_job.this.name
    REDSHIFT_ACC_COPY_JOB_OTHER_DATABASE    = redshift_database.copy_other.name
    REDSHIFT_ACC_SHARED_PRODUCER_CLUSTER    = aws_redshift_cluster.producer.cluster_identifier
    REDSHIFT_ACC_SHARED_PRODUCER_SECRET_ARN = aws_redshift_cluster.producer.master_password_secret_arn
    REDSHIFT_ACC_SHARED_PRODUCER_NAMESPACE  = split(":", aws_redshift_cluster.producer.cluster_namespace_arn)[6]
    REDSHIFT_ACC_SHARED_CONSUMER_NAMESPACE  = aws_redshiftserverless_namespace.this.namespace_id
    REDSHIFT_ACC_SHARED_DATASHARE           = redshift_datashare.this.name
    REDSHIFT_ACC_SHARED_DATASHARE_ARN       = redshift_database.shared.datashare_arn
    REDSHIFT_ACC_SHARED_DATABASE            = redshift_database.shared.name
    REDSHIFT_ACC_SHARED_SCHEMA              = redshift_schema.source.name
    REDSHIFT_ACC_SHARED_TABLE               = redshift_table.source.name
    REDSHIFT_ACC_SHARED_WRITE               = "1"
    REDSHIFT_ACC_USER_LOCKOUTS              = "1"
    REDSHIFT_ACC_SYSTEM_SETTINGS            = "1"
  }
}
