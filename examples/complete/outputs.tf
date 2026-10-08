output "shared_database" {
  description = "Name of the consumer database backed by the producer datashare."
  value       = data.redshift_database.shared.name
}

output "shared_database_metadata" {
  description = "Read-only database attributes and import-compatible identity."
  value = {
    id                 = data.redshift_database.shared.id
    name               = data.redshift_database.shared.name
    datashare_arn      = data.redshift_database.shared.datashare_arn
    database_type      = data.redshift_database.shared.database_type
    with_permissions   = data.redshift_database.shared.with_permissions
    share_name         = data.redshift_database.shared.share_name
    producer_account   = data.redshift_database.shared.producer_account
    producer_namespace = data.redshift_database.shared.producer_namespace
  }
}

output "producer_share" {
  description = "Producer share name and public-access setting."
  value = {
    name                = data.redshift_datashare.producer.name
    publicly_accessible = data.redshift_datashare.producer.publicly_accessible
  }
}

output "reader" {
  description = "Non-secret reader identity and its configured role."
  value = {
    name = data.redshift_user.reader.name
    role = data.redshift_role.readers.name
  }
}

output "reader_group" {
  description = "SQL user group discovered by the read-only lookup."
  value       = data.redshift_group.readers.name
}

output "schema_owner" {
  description = "Owner discovered from the local-schema lookup."
  value       = data.redshift_schema.local.owner
}

output "external_glue_database" {
  description = "AWS Glue database bound to the example external schema."
  value       = data.redshift_external_schema.glue.glue_database
}

output "catalog_checks" {
  description = "Read-only observations covering every relationship, permission, and annotation data source."
  value = {
    group_membership = data.redshift_group_membership.reader.exists
    role_grants = {
      reader    = data.redshift_role_grant.reader.exists
      operators = data.redshift_role_grant.operators.exists
    }
    producer_membership = {
      schema         = data.redshift_datashare_schema.source.exists
      include_new    = data.redshift_datashare_schema.source.include_new
      table          = data.redshift_datashare_table.source.exists
      consumer_grant = data.redshift_datashare_grant.consumer.exists
    }
    shared_privileges  = { for scope, grant in data.redshift_grant.shared_read : scope => grant.privileges }
    local_privileges   = data.redshift_grant.local_schema.privileges
    object_privileges  = data.redshift_object_grant.group_schema.privileges
    system_privileges  = data.redshift_system_grant.operators.privileges
    iam_commands       = data.redshift_assumerole_grant.reader.privileges
    default_privileges = data.redshift_default_privileges.reader_tables.privileges
    schema_comment     = data.redshift_comment.local_schema.text
  }
}

output "identity_namespace" {
  description = "Configured SQL identity namespace as observed by the data source."
  value       = try(data.redshift_identity_provider.this[0].namespace, null)
}

output "warehouses" {
  description = "Created warehouse endpoints and administrator secret ARNs; no password values are exported."
  # AWS marks administrator metadata sensitive; this object contains identifiers only, never password values.
  value = {
    producer = {
      type             = "provisioned"
      cluster          = aws_redshift_cluster.producer.cluster_identifier
      namespace_arn    = aws_redshift_cluster.producer.cluster_namespace_arn
      host             = aws_redshift_cluster.producer.dns_name
      port             = aws_redshift_cluster.producer.port
      admin_database   = aws_redshift_cluster.producer.database_name
      admin_username   = try(nonsensitive(aws_redshift_cluster.producer.master_username), aws_redshift_cluster.producer.master_username)
      admin_secret_arn = try(nonsensitive(aws_redshift_cluster.producer.master_password_secret_arn), aws_redshift_cluster.producer.master_password_secret_arn)
      vpc_id           = module.producer_vpc.vpc_id
    }
    consumer = {
      type             = "serverless"
      workgroup        = aws_redshiftserverless_workgroup.consumer.workgroup_name
      namespace_arn    = aws_redshiftserverless_namespace.consumer.arn
      host             = aws_redshiftserverless_workgroup.consumer.endpoint[0].address
      port             = aws_redshiftserverless_workgroup.consumer.endpoint[0].port
      admin_database   = aws_redshiftserverless_namespace.consumer.db_name
      admin_username   = nonsensitive(aws_redshiftserverless_namespace.consumer.admin_username)
      admin_secret_arn = nonsensitive(aws_redshiftserverless_namespace.consumer.admin_password_secret_arn)
      vpc_id           = module.consumer_vpc.vpc_id
    }
  }
}

output "fixture" {
  description = "Owned native and external sample data for SQL checks."
  value = {
    producer_database = data.redshift_database.producer.name
    producer_table    = "${redshift_datashare_table.source.schema}.${redshift_datashare_table.source.table}"
    datashare_arn     = local.datashare_arn
    bucket            = module.fixture_bucket.s3_bucket_id
    glue_database     = aws_glue_catalog_database.fixture.name
    reader_secret_arn = module.reader_secret.secret_arn
  }
}

output "verification" {
  description = "Successful live SELECT checks run during apply; statement IDs can be used to retrieve the result rows."
  value = {
    shared_statement_id   = aws_redshiftdata_statement.verify_shared.id
    spectrum_statement_id = aws_redshiftdata_statement.verify_spectrum.id
  }
}

output "connection_checks" {
  description = "Observed administration databases for enabled connection probes; disabled probes are null."
  value = {
    producer_data_api_iam    = try(data.redshift_database.producer_data_api_iam[0].name, null)
    consumer_data_api_iam    = try(data.redshift_database.consumer_data_api_iam[0].name, null)
    producer_direct_iam      = try(data.redshift_database.producer_direct_iam[0].name, null)
    consumer_direct_iam      = try(data.redshift_database.consumer_direct_iam[0].name, null)
    producer_direct_password = try(data.redshift_database.producer_direct_password[0].name, null)
    consumer_direct_password = try(data.redshift_database.consumer_direct_password[0].name, null)
  }
}

output "identity_center" {
  description = "Optional SSO application and group information; null when no instance ARN is provided."
  value = local.sso_enabled ? {
    instance_arn       = var.identity_center_instance_arn
    application_arn    = aws_redshift_idc_application.this[0].idc_managed_application_arn
    namespace          = data.redshift_identity_provider.this[0].namespace
    readers_group_id   = aws_identitystore_group.readers[0].group_id
    operators_group_id = aws_identitystore_group.operators[0].group_id
    test_user_id       = var.identity_center_test_user_id
  } : null
}
