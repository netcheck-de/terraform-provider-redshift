# Mocked applies exercise dependent catalog reads without AWS or SQL operations.
mock_provider "aws" {
  alias  = "producer"
  source = "./tests/mocks/aws_producer"
}

mock_provider "aws" {
  alias  = "consumer"
  source = "./tests/mocks/aws_consumer"
}

mock_provider "random" {
  source = "./tests/mocks/random"
}
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

# Endpoint discovery must return stable service names across successive mocked applies.
override_data {
  target = module.producer_endpoints.data.aws_vpc_endpoint_service.this["s3"]
  values = { service_name = "com.amazonaws.eu-central-1.s3" }
}
override_data {
  target = module.producer_endpoints.data.aws_vpc_endpoint_service.this["glue"]
  values = { service_name = "com.amazonaws.eu-central-1.glue" }
}
override_data {
  target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["s3"]
  values = { service_name = "com.amazonaws.eu-central-1.s3" }
}
override_data {
  target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["glue"]
  values = { service_name = "com.amazonaws.eu-central-1.glue" }
}

# Observed metadata deliberately differs from managed resources where appropriate.
override_data {
  target = data.redshift_database.shared
  values = {
    id                 = "database-lookup-identity"
    datashare_arn      = "arn:aws:redshift:eu-central-1:111111111111:datashare:11111111-2222-3333-4444-555555555555/example_share"
    database_type      = "shared"
    with_permissions   = true
    share_name         = "example_share"
    producer_account   = "111111111111"
    producer_namespace = "11111111-2222-3333-4444-555555555555"
  }
}
override_data {
  target = data.redshift_schema.local
  values = { owner = "observed_schema_owner" }
}
override_data {
  target = data.redshift_external_schema.glue
  values = { glue_database = "observed_glue_catalog" }
}
override_data {
  target = data.redshift_datashare.producer
  values = { publicly_accessible = false }
}
override_data {
  target = data.redshift_assumerole_grant.reader
  values = { privileges = ["COPY", "UNLOAD"] }
}

run "private_same_account_defaults" {
  command = apply

  assert {
    condition = (
      startswith(aws_redshift_cluster.producer.node_type, "ra3.") &&
      # The AWS provider's non-computed encrypted default is absent from mock state.
      aws_redshift_cluster.producer.manage_master_password && aws_redshift_cluster.producer.cluster_type == "single-node" &&
      aws_redshiftserverless_namespace.consumer.manage_admin_password &&
      aws_redshift_cluster.producer.default_iam_role_arn == aws_iam_role.producer.arn &&
      aws_redshiftserverless_namespace.consumer.default_iam_role_arn == aws_iam_role.consumer.arn &&
      redshift_external_schema.glue.iam_role_arn == aws_iam_role.producer.arn &&
      redshift_external_schema.glue.glue_database == aws_glue_catalog_database.fixture.name &&
      !aws_redshift_cluster.producer.publicly_accessible && !aws_redshiftserverless_workgroup.consumer.publicly_accessible &&
      aws_redshift_cluster.producer.enhanced_vpc_routing && aws_redshiftserverless_workgroup.consumer.enhanced_vpc_routing &&
      module.producer_vpc.vpc_id != module.consumer_vpc.vpc_id &&
      length(module.producer_vpc.intra_subnets) == 3 && length(module.consumer_vpc.intra_subnets) == 3 &&
      toset(module.producer_vpc.intra_subnets_cidr_blocks) == toset(local.producer_subnet_cidrs) &&
      toset(module.consumer_vpc.intra_subnets_cidr_blocks) == toset(local.consumer_subnet_cidrs) &&
      aws_security_group.producer.vpc_id == module.producer_vpc.vpc_id &&
      aws_security_group.consumer.vpc_id == module.consumer_vpc.vpc_id &&
      toset(aws_redshift_subnet_group.producer.subnet_ids) == toset(module.producer_vpc.intra_subnets) &&
      toset(aws_redshiftserverless_workgroup.consumer.subnet_ids) == toset(module.consumer_vpc.intra_subnets)
    )
    error_message = "Defaults must provision independent private RA3/Serverless warehouses with managed passwords and three attached subnets each."
  }
  assert {
    condition = (
      module.producer_vpc.igw_id == null && module.consumer_vpc.igw_id == null &&
      length(module.producer_vpc.public_subnets) == 0 && length(module.consumer_vpc.public_subnets) == 0 &&
      length(module.producer_vpc.natgw_ids) == 0 && length(module.consumer_vpc.natgw_ids) == 0 &&
      length(aws_vpc_security_group_ingress_rule.producer_sql) == 0 && length(aws_vpc_security_group_ingress_rule.consumer_sql) == 0 &&
      anytrue([for p in aws_redshift_parameter_group.producer.parameter : p.name == "require_ssl" && p.value == "true"]) &&
      anytrue([for p in aws_redshiftserverless_workgroup.consumer.config_parameter : p.parameter_key == "require_ssl" && p.parameter_value == "true"]) &&
      aws_s3_object.fixture.bucket == module.fixture_bucket.s3_bucket_id && aws_s3_object.fixture.server_side_encryption == "AES256" &&
      toset(jsondecode(local.fixture_bucket_policy).Statement[0].Resource) == toset(["arn:aws:s3:::mock-fixture", "arn:aws:s3:::mock-fixture/*"]) &&
      jsondecode(aws_iam_role_policy.producer_fixture.policy).Statement[2].Resource[0] == "arn:aws:glue:eu-central-1:111111111111:catalog" &&
      jsondecode(aws_iam_role_policy.producer_fixture.policy).Statement[2].Resource[2] == "arn:aws:glue:eu-central-1:111111111111:table/mock_fixture/fixture" &&
      anytrue([for s in jsondecode(local.fixture_bucket_policy).Statement :
        s.Effect == "Deny" && try(s.Condition.Bool["aws:SecureTransport"], null) == "false"
      ])
    )
    error_message = "Private defaults must omit internet/NAT gateways and SQL ingress, require warehouse TLS, and encrypt fixtures with a TLS-only bucket policy."
  }
  assert {
    condition = (
      toset(keys(module.producer_endpoints.endpoints)) == toset(["s3", "glue"]) &&
      toset(keys(module.consumer_endpoints.endpoints)) == toset(["s3", "glue"]) &&
      module.producer_endpoints.endpoints["s3"].vpc_endpoint_type == "Gateway" &&
      module.consumer_endpoints.endpoints["s3"].vpc_endpoint_type == "Gateway" &&
      module.producer_endpoints.endpoints["s3"].service_name == "com.amazonaws.eu-central-1.s3" &&
      module.consumer_endpoints.endpoints["s3"].service_name == "com.amazonaws.eu-central-1.s3" &&
      module.producer_endpoints.endpoints["glue"].service_name == "com.amazonaws.eu-central-1.glue" &&
      module.consumer_endpoints.endpoints["glue"].service_name == "com.amazonaws.eu-central-1.glue" &&
      toset(module.producer_endpoints.endpoints["s3"].route_table_ids) == toset(module.producer_vpc.intra_route_table_ids) &&
      toset(module.consumer_endpoints.endpoints["s3"].route_table_ids) == toset(module.consumer_vpc.intra_route_table_ids) &&
      module.producer_endpoints.endpoints["glue"].private_dns_enabled && module.consumer_endpoints.endpoints["glue"].private_dns_enabled &&
      toset(module.producer_endpoints.endpoints["glue"].subnet_ids) == toset(local.producer_subnet_ids) &&
      toset(module.consumer_endpoints.endpoints["glue"].subnet_ids) == toset(local.consumer_subnet_ids) &&
      aws_vpc_security_group_egress_rule.producer_s3.prefix_list_id == module.producer_endpoints.endpoints["s3"].prefix_list_id &&
      aws_vpc_security_group_egress_rule.consumer_s3.prefix_list_id == module.consumer_endpoints.endpoints["s3"].prefix_list_id
    )
    error_message = "Private fixtures must use S3 gateway routes and private Glue endpoints in the warehouse subnets, with prefix-list egress."
  }
  assert {
    condition = (
      redshift_datashare_grant.consumer.namespace_id == aws_redshiftserverless_namespace.consumer.namespace_id &&
      redshift_datashare_grant.consumer.account_id == null &&
      length(aws_redshift_data_share_authorization.consumer) == 0 && length(aws_redshift_data_share_consumer_association.this) == 0 &&
      local.datashare_arn == "arn:aws:redshift:eu-central-1:111111111111:datashare:11111111-2222-3333-4444-555555555555/example_share" &&
      redshift_database.shared.datashare_arn == local.datashare_arn && redshift_database.shared.with_permissions &&
      redshift_database.shared.timeouts.create == "15m" &&
      !redshift_datashare.producer.publicly_accessible && redshift_datashare_schema.source.include_new &&
      redshift_datashare_table.source.schema == "public" && redshift_datashare_table.source.table == "fixture"
    )
    error_message = "Same-account sharing must grant only the consumer namespace and derive the share ARN from the provisioned producer namespace."
  }
  assert {
    condition = (
      redshift_role.readers.name == "example_readers" && redshift_role.operators.name == "example_operators" &&
      redshift_role_grant.reader.role == redshift_role.readers.name && redshift_role_grant.reader.to_user == redshift_user.reader.name &&
      redshift_role_grant.operators.role == "sys:operator" && redshift_role_grant.operators.to_role == redshift_role.operators.name &&
      redshift_group_membership.reader.group == redshift_group.readers.name && redshift_group_membership.reader.user == redshift_user.reader.name &&
      redshift_object_grant.group_schema.grantee == redshift_group.readers.name && redshift_object_grant.group_schema.grantee_type == "GROUP" &&
      toset(keys(redshift_grant.shared_read)) == toset(["DATABASE", "SCHEMAS", "TABLES"]) &&
      redshift_grant.shared_read["DATABASE"].privileges == toset(["USAGE"]) &&
      redshift_grant.shared_read["SCHEMAS"].privileges == toset(["USAGE"]) && redshift_grant.shared_read["TABLES"].privileges == toset(["SELECT"]) &&
      alltrue([for scope, grant in redshift_grant.shared_read : grant.database_name == redshift_database.shared.name && grant.role == redshift_role.readers.name && grant.scope == scope])
    )
    error_message = "Ordinary reader/operator roles must wire users, groups, and least-privilege shared-database access without SSO."
  }
  assert {
    condition = (
      redshift_user.reader.password_wo_version == 1 &&
      redshift_user.loader.create_database && !redshift_user.loader.superuser && redshift_user.loader.password_wo_version == 1 &&
      redshift_system_grant.operators.privileges == toset(["CREATE ROLE"]) &&
      redshift_grant.local_schema_tables.scope == "TABLES" && redshift_grant.local_schema_tables.schema_name == redshift_schema.local.name &&
      redshift_grant.local_schema_functions.scope == "FUNCTIONS" && redshift_grant.local_schema_functions.role == redshift_role.readers.name &&
      redshift_grant.local_schema_procedures.scope == "PROCEDURES" && redshift_grant.local_schema_procedures.role == redshift_role.operators.name &&
      redshift_object_grant.loader_events.object_type == "TABLE" && redshift_object_grant.loader_events.object_name == local.local_table_name &&
      redshift_object_grant.loader_events.grantee_type == "USER" && redshift_object_grant.loader_events.grantee == redshift_user.loader.name &&
      redshift_object_grant.operators_database.object_type == "DATABASE" && redshift_object_grant.operators_database.grantee_type == "ROLE" &&
      redshift_object_grant.public_schema_usage.grantee_type == "PUBLIC" && redshift_object_grant.public_schema_usage.grantee == "public" &&
      redshift_default_privilege.reader_tables.grantee_type == "GROUP" &&
      redshift_default_privilege.reader_schema_tables.schema_name == redshift_schema.local.name &&
      redshift_default_privilege.reader_schema_tables.grantee_type == "ROLE" &&
      toset(keys(redshift_default_privilege.operator_routines)) == toset(["FUNCTIONS", "PROCEDURES"]) &&
      length(redshift_assumerole_grant.loader) == 1 && redshift_assumerole_grant.loader[0].iam_role_arn == aws_iam_role.consumer.arn &&
      redshift_assumerole_grant.loader[0].grantee_type == "USER"
    )
    error_message = "User flags, scoped routine grants, object grants for every grantee type, and default privileges must be wired."
  }
  assert {
    condition = (
      toset([for comment in [redshift_comment.local_schema, redshift_comment.local_database, redshift_comment.local_table, redshift_comment.local_column, redshift_comment.local_view] : comment.object_type]) == toset(["SCHEMA", "DATABASE", "TABLE", "COLUMN", "VIEW"]) &&
      redshift_comment.local_column.column_name == "label" && redshift_comment.local_view.object_name == local.local_view_name &&
      aws_redshiftdata_statement.local_table.database == redshift_database.local.name &&
      strcontains(aws_redshiftdata_statement.local_view.sql, "public.${local.local_table_name}") &&
      redshift_external_schema.glue.region == var.region && redshift_external_schema.glue.refresh_revision == "1" &&
      toset(keys(redshift_grant.share_schema)) == toset(["SCHEMA", "TABLES"]) &&
      alltrue([for scope, grant in redshift_grant.share_schema : grant.datashare == redshift_datashare.grants.name && grant.role == null]) &&
      redshift_grant.share_schema["TABLES"].privileges == toset(["SELECT"])
    )
    error_message = "Every comment target type, the external schema revision, and datashare-recipient grants must be wired."
  }
  assert {
    condition = (
      length(aws_redshift_idc_application.this) == 0 && length(redshift_identity_provider.this) == 0 && length(data.redshift_identity_provider.this) == 0 &&
      length(data.aws_ssoadmin_instances.this) == 0 && length(aws_iam_role_policy.identity_center) == 0 &&
      length(aws_identitystore_group.readers) == 0 && length(aws_identitystore_group.operators) == 0 && length(aws_identitystore_group_membership.reader) == 0 &&
      length(aws_ssoadmin_application_assignment.readers) == 0 && length(aws_ssoadmin_application_assignment.operators) == 0 &&
      length(redshift_role.sso_readers) == 0 && length(redshift_role.sso_operators) == 0 &&
      length(redshift_role_grant.sso_readers) == 0 && length(redshift_role_grant.sso_operators) == 0 &&
      !contains(keys(module.consumer_endpoints.endpoints), "sso_oauth") && !contains(keys(module.consumer_endpoints.endpoints), "identitystore") &&
      output.identity_center == null && output.identity_namespace == null
    )
    error_message = "A null instance ARN must skip the entire SSO graph."
  }
  assert {
    condition = (
      var.connection_checks == toset(["producer_data_api_iam", "consumer_data_api_iam"]) &&
      length(data.redshift_database.producer_data_api_iam) == 1 && length(data.redshift_database.consumer_data_api_iam) == 1 &&
      output.connection_checks.producer_data_api_iam == var.admin_database &&
      output.connection_checks.consumer_data_api_iam == var.admin_database &&
      alltrue([for mode, name in output.connection_checks : name == null if strcontains(mode, "direct")]) &&
      length(redshift_copy_job.fixture) == 1 && !redshift_copy_job.fixture[0].auto &&
      length(aws_redshift_integration.copy_job) == 1 && length(redshift_table.copy_target) == 1 &&
      length(aws_dynamodb_table.zero_etl_source) == 1 && length(aws_redshift_integration.zero_etl) == 1 &&
      length(redshift_database.zero_etl) == 1 &&
      redshift_database.zero_etl[0].integration.refresh_interval == 900 &&
      !redshift_database.zero_etl[0].integration.auto_remediation
    )
    error_message = "Defaults must include both Data API IAM probes, a non-AUTO COPY JOB, and an owned DynamoDB zero-ETL destination without enabling direct SQL."
  }
  assert {
    condition = (
      length(redshift_assumerole_grant.reader) == 1 && length(aws_redshiftdata_statement.assumerole_policy) == 1 &&
      redshift_assumerole_grant.reader[0].iam_role_arn == "DEFAULT" && redshift_assumerole_grant.reader[0].grantee == redshift_role.readers.name &&
      redshift_assumerole_grant.reader[0].grantee_type == "ROLE" && redshift_assumerole_grant.reader[0].privileges == toset(["COPY", "UNLOAD"]) &&
      aws_redshiftdata_statement.assumerole_policy[0].database == aws_redshiftserverless_namespace.consumer.db_name &&
      aws_redshiftdata_statement.assumerole_policy[0].secret_arn == aws_redshiftserverless_namespace.consumer.admin_password_secret_arn &&
      output.catalog_checks.iam_commands == toset(["COPY", "UNLOAD"])
    )
    error_message = "Default ASSUMEROLE must initialize the owned warehouse and grant COPY/UNLOAD only to the reader role."
  }
  assert {
    condition = (
      aws_redshiftdata_statement.source_table.database == redshift_database.producer.name &&
      aws_redshiftdata_statement.source_seed.database == redshift_database.producer.name &&
      aws_redshiftdata_statement.source_table.secret_arn == aws_redshift_cluster.producer.master_password_secret_arn &&
      aws_redshiftdata_statement.verify_shared.database == aws_redshiftserverless_namespace.consumer.db_name &&
      aws_redshiftdata_statement.verify_shared.secret_arn == module.reader_secret.secret_arn &&
      aws_redshiftdata_statement.verify_shared.secret_arn != aws_redshiftserverless_namespace.consumer.admin_password_secret_arn &&
      strcontains(aws_redshiftdata_statement.verify_shared.sql, "${redshift_database.shared.name}.public.fixture") &&
      aws_redshiftdata_statement.verify_spectrum.database == redshift_external_schema.glue.database &&
      aws_redshiftdata_statement.verify_spectrum.secret_arn == aws_redshift_cluster.producer.master_password_secret_arn &&
      alltrue([for sql in [aws_redshiftdata_statement.verify_shared.sql, aws_redshiftdata_statement.verify_spectrum.sql] :
        strcontains(sql, "1 / CASE") && strcontains(sql, "COUNT(*) = 2") && strcontains(sql, "ELSE 0")
      ]) &&
      output.verification.shared_statement_id == aws_redshiftdata_statement.verify_shared.id &&
      output.verification.spectrum_statement_id == aws_redshiftdata_statement.verify_spectrum.id
    )
    error_message = "Owned source initialization and arithmetic row checks must use the intended databases and reader/admin secrets."
  }
  assert {
    condition = (
      output.shared_database_metadata == {
        id               = "database-lookup-identity", name = redshift_database.shared.name, datashare_arn = local.datashare_arn,
        database_type    = "shared", with_permissions = true, share_name = "example_share",
        producer_account = "111111111111", producer_namespace = "11111111-2222-3333-4444-555555555555"
      } &&
      output.schema_owner == "observed_schema_owner" && output.external_glue_database == "observed_glue_catalog" &&
      output.producer_share == { name = redshift_datashare.producer.name, publicly_accessible = false } &&
      output.reader == { name = data.redshift_user.reader.name, role = data.redshift_role.readers.name } &&
      output.warehouses.producer.admin_secret_arn == aws_redshift_cluster.producer.master_password_secret_arn &&
      output.warehouses.consumer.admin_secret_arn == aws_redshiftserverless_namespace.consumer.admin_password_secret_arn &&
      output.warehouses.producer.host == "producer.example.test" && output.warehouses.consumer.host == "consumer.example.test" &&
      output.fixture.reader_secret_arn == module.reader_secret.secret_arn
    )
    error_message = "Outputs must assemble actual lookup metadata, managed administrator secrets, endpoints, and verification identities."
  }
}

run "connection_checks_disabled" {
  command = apply
  variables { connection_checks = [] }

  assert {
    condition = (
      length(data.redshift_database.producer_data_api_iam) == 0 && length(data.redshift_database.consumer_data_api_iam) == 0 &&
      alltrue([for name in values(output.connection_checks) : name == null])
    )
    error_message = "An empty connection_checks set must skip the default IAM probes and all direct probes."
  }
}

run "assumerole_disabled" {
  command = apply
  variables { enable_assumerole_grant = false }
  override_data {
    target = data.redshift_assumerole_grant.reader
    values = { privileges = [] }
  }
  assert {
    condition = (
      length(redshift_assumerole_grant.reader) == 0 && length(aws_redshiftdata_statement.assumerole_policy) == 0 &&
      data.redshift_assumerole_grant.reader.iam_role_arn == "DEFAULT" && data.redshift_assumerole_grant.reader.grantee == redshift_role.readers.name &&
      length(output.catalog_checks.iam_commands) == 0 && length(redshift_grant.shared_read) == 3
    )
    error_message = "Disabling ASSUMEROLE must omit both policy initialization and managed grants while retaining ordinary readers and the lookup."
  }
}

run "identity_center_enabled" {
  command   = apply
  state_key = "identity_center"
  variables {
    identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
    identity_center_test_user_id = "12345678-1234-1234-1234-123456789abc"
  }
  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }
  override_data {
    target = data.redshift_identity_provider.this[0]
    values = {
      type            = "AWSIDC"
      enabled         = true
      namespace       = "redshift_oss_e2e_deadbeef"
      application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef"
      iam_role_arn    = "arn:aws:iam::111111111111:role/consumer-fixture"
    }
  }
  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["sso_oauth"]
    values = { service_name = "com.amazonaws.eu-central-1.sso-oauth" }
  }
  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["identitystore"]
    values = { service_name = "com.amazonaws.eu-central-1.identitystore" }
  }
  assert {
    condition = (
      length(aws_redshift_idc_application.this) == 1 && length(redshift_identity_provider.this) == 1 &&
      redshift_identity_provider.this[0].application_arn == aws_redshift_idc_application.this[0].idc_managed_application_arn &&
      redshift_identity_provider.this[0].iam_role_arn == aws_iam_role.consumer.arn &&
      aws_identitystore_group.readers[0].identity_store_id == "d-1234567890" && aws_identitystore_group.operators[0].identity_store_id == "d-1234567890" &&
      aws_identitystore_group_membership.reader[0].member_id == var.identity_center_test_user_id &&
      aws_identitystore_group_membership.reader[0].group_id == aws_identitystore_group.readers[0].group_id &&
      aws_ssoadmin_application_assignment.readers[0].application_arn == aws_redshift_idc_application.this[0].idc_managed_application_arn &&
      aws_ssoadmin_application_assignment.operators[0].application_arn == aws_redshift_idc_application.this[0].idc_managed_application_arn &&
      aws_ssoadmin_application_assignment.readers[0].principal_id == aws_identitystore_group.readers[0].group_id &&
      aws_ssoadmin_application_assignment.operators[0].principal_id == aws_identitystore_group.operators[0].group_id &&
      contains(aws_redshiftserverless_namespace.consumer.iam_roles, aws_iam_role.consumer.arn) &&
      toset(keys(module.consumer_endpoints.endpoints)) == toset(["s3", "glue", "sso_oauth", "identitystore"]) &&
      module.consumer_endpoints.endpoints["sso_oauth"].private_dns_enabled && module.consumer_endpoints.endpoints["identitystore"].private_dns_enabled &&
      module.consumer_endpoints.endpoints["sso_oauth"].service_name == "com.amazonaws.eu-central-1.sso-oauth" &&
      module.consumer_endpoints.endpoints["identitystore"].service_name == "com.amazonaws.eu-central-1.identitystore" &&
      toset(module.consumer_endpoints.endpoints["sso_oauth"].subnet_ids) == toset(local.consumer_subnet_ids) &&
      toset(module.consumer_endpoints.endpoints["identitystore"].subnet_ids) == toset(local.consumer_subnet_ids)
    )
    error_message = "SSO must connect the discovered store, optional existing user, group assignments, managed application, and namespace IAM role."
  }
  assert {
    condition = (
      redshift_role.readers.name == "example_readers" && redshift_role.operators.name == "example_operators" &&
      redshift_role.sso_readers[0].name == "${redshift_identity_provider.this[0].namespace}:${aws_identitystore_group.readers[0].display_name}" &&
      redshift_role.sso_operators[0].name == "${redshift_identity_provider.this[0].namespace}:${aws_identitystore_group.operators[0].display_name}" &&
      redshift_role_grant.sso_readers[0].role == redshift_role.readers.name && redshift_role_grant.sso_readers[0].to_role == redshift_role.sso_readers[0].name &&
      redshift_role_grant.sso_operators[0].role == redshift_role.operators.name && redshift_role_grant.sso_operators[0].to_role == redshift_role.sso_operators[0].name &&
      output.identity_namespace == "redshift_oss_e2e_deadbeef" && output.identity_center == {
        instance_arn       = var.identity_center_instance_arn, application_arn = aws_redshift_idc_application.this[0].idc_managed_application_arn,
        namespace          = "redshift_oss_e2e_deadbeef", readers_group_id = aws_identitystore_group.readers[0].group_id,
        operators_group_id = aws_identitystore_group.operators[0].group_id, test_user_id = var.identity_center_test_user_id
      }
    )
    error_message = "SSO group roles must inherit unchanged ordinary roles and outputs must expose observed namespace and created groups."
  }
}

run "identity_center_existing_groups" {
  command   = apply
  state_key = "identity_center_existing"
  variables {
    identity_center_instance_arn        = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
    identity_center_reader_group_name   = "existing-developers"
    identity_center_operator_group_name = "existing-devops"
    identity_center_test_user_id        = "12345678-1234-1234-1234-123456789abc"
  }
  override_data {
    target = data.aws_identitystore_group.readers[0]
    values = { group_id = "33333333-3333-3333-3333-333333333333", display_name = "existing-developers" }
  }
  override_data {
    target = data.aws_identitystore_group.operators[0]
    values = { group_id = "44444444-4444-4444-4444-444444444444", display_name = "existing-devops" }
  }
  override_data {
    target = data.aws_identitystore_group.verify_consumer["readers"]
    values = { display_name = "existing-developers" }
  }
  override_data {
    target = data.aws_identitystore_group.verify_consumer["operators"]
    values = { display_name = "existing-devops" }
  }
  override_data {
    target = data.aws_ssoadmin_application_assignments.verify_consumer[0]
    values = {
      application_assignments = [
        { principal_type = "GROUP", principal_id = "33333333-3333-3333-3333-333333333333", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
        { principal_type = "GROUP", principal_id = "44444444-4444-4444-4444-444444444444", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
      ]
    }
  }
  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }
  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["sso_oauth"]
    values = { service_name = "com.amazonaws.eu-central-1.sso-oauth" }
  }
  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["identitystore"]
    values = { service_name = "com.amazonaws.eu-central-1.identitystore" }
  }
  assert {
    condition = (
      length(aws_identitystore_group.readers) == 0 && length(aws_identitystore_group.operators) == 0 &&
      length(aws_identitystore_group_membership.reader) == 0 &&
      aws_ssoadmin_application_assignment.readers[0].principal_id == "33333333-3333-3333-3333-333333333333" &&
      aws_ssoadmin_application_assignment.operators[0].principal_id == "44444444-4444-4444-4444-444444444444" &&
      redshift_role.sso_readers[0].name == "${redshift_identity_provider.this[0].namespace}:existing-developers" &&
      redshift_role.sso_operators[0].name == "${redshift_identity_provider.this[0].namespace}:existing-devops" &&
      output.identity_center.readers_group_id == "33333333-3333-3333-3333-333333333333" && output.identity_center.operators_group_id == "44444444-4444-4444-4444-444444444444"
    )
    error_message = "Existing groups must be looked up and assigned without creating groups or changing their membership."
  }
}

run "rejects_single_existing_group" {
  command = plan
  variables {
    identity_center_instance_arn      = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
    identity_center_reader_group_name = "existing-developers"
  }
  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }
  expect_failures = [var.identity_center_reader_group_name]
}

run "cross_account_sharing" {
  command = apply
  # A different account is a separate deployment, not a credential migration of the preceding SSO environment.
  state_key = "cross_account"
  override_data {
    target = data.aws_caller_identity.consumer
    values = { account_id = "222222222222" }
  }
  override_resource {
    target = aws_iam_role.consumer
    values = { id = "redshift-oss-e2e-deadbeef-consumer", arn = "arn:aws:iam::222222222222:role/consumer-fixture" }
  }
  override_resource {
    target = aws_iam_role.extfunctions_lambda
    values = { id = "redshift-oss-e2e-deadbeef-udf-exec", arn = "arn:aws:iam::222222222222:role/lambda-udf-exec" }
  }
  override_resource {
    target = aws_redshiftserverless_namespace.consumer
    values = {
      namespace_id              = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      arn                       = "arn:aws:redshift-serverless:eu-central-1:222222222222:namespace/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      admin_password_secret_arn = "arn:aws:secretsmanager:eu-central-1:222222222222:secret:consumer-admin-ABC123"
    }
  }
  override_resource {
    target = aws_dynamodb_table.zero_etl_source[0]
    values = { arn = "arn:aws:dynamodb:eu-central-1:222222222222:table/example-zero-etl-source" }
  }
  override_resource {
    target = aws_redshiftserverless_workgroup.consumer
    values = {
      arn      = "arn:aws:redshift-serverless:eu-central-1:222222222222:workgroup/bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
      port     = 5439
      endpoint = [{ address = "consumer.example.test", port = 5439 }]
    }
  }
  override_resource {
    target = aws_lambda_function.extfunctions_upper
    values = { arn = "arn:aws:lambda:eu-central-1:222222222222:function:redshift-oss-e2e-deadbeef-upper" }
  }
  override_resource {
    target = aws_redshift_integration.zero_etl[0]
    values = { arn = "arn:aws:redshift:eu-central-1:222222222222:integration:a1b2c3d4-5678-90ab-cdef-example11111" }
  }
  override_resource {
    target = module.reader_secret.aws_secretsmanager_secret.this[0]
    values = { arn = "arn:aws:secretsmanager:eu-central-1:222222222222:secret:reader-ABC123" }
  }
  override_data {
    target = data.aws_vpc.verify_consumer
    values = { cidr_block = "10.43.0.0/16", owner_id = "222222222222", enable_dns_support = true, enable_dns_hostnames = true }
  }
  override_data {
    target = data.aws_subnet.verify_consumer["0"]
    values = { vpc_id = "vpc-consumer", cidr_block = "10.43.0.0/24", availability_zone = "eu-central-1a", owner_id = "222222222222", map_public_ip_on_launch = false }
  }
  override_data {
    target = data.aws_subnet.verify_consumer["1"]
    values = { vpc_id = "vpc-consumer", cidr_block = "10.43.1.0/24", availability_zone = "eu-central-1b", owner_id = "222222222222", map_public_ip_on_launch = false }
  }
  override_data {
    target = data.aws_subnet.verify_consumer["2"]
    values = { vpc_id = "vpc-consumer", cidr_block = "10.43.2.0/24", availability_zone = "eu-central-1c", owner_id = "222222222222", map_public_ip_on_launch = false }
  }
  override_data {
    target = data.aws_redshiftserverless_namespace.verify_consumer
    values = {
      arn                  = "arn:aws:redshift-serverless:eu-central-1:222222222222:namespace/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      namespace_id         = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      db_name              = "dev", admin_username = "tfadmin"
      default_iam_role_arn = "arn:aws:iam::222222222222:role/consumer-fixture"
      iam_roles            = ["arn:aws:iam::222222222222:role/consumer-fixture"]
    }
  }
  override_data {
    target = data.aws_iam_role.verify_consumer
    values = {
      arn = "arn:aws:iam::222222222222:role/consumer-fixture"
      assume_role_policy = jsonencode({
        Version = "2012-10-17"
        Statement = [{
          Effect    = "Allow", Principal = { Service = ["redshift.amazonaws.com", "redshift-serverless.amazonaws.com"] }
          Action    = ["sts:AssumeRole", "sts:SetContext"]
          Condition = { StringEquals = { "aws:SourceAccount" = "222222222222" } }
        }]
      })
    }
  }
  override_data {
    target = data.aws_iam_role.verify_lambda
    values = {
      arn = "arn:aws:iam::222222222222:role/lambda-udf-exec"
      assume_role_policy = jsonencode({
        Version   = "2012-10-17"
        Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole" }]
      })
    }
  }
  override_data {
    target = data.aws_lambda_function.verify_upper
    values = { role = "arn:aws:iam::222222222222:role/lambda-udf-exec", runtime = "python3.12", handler = "index.handler", code_sha256 = "uQKxXeOixWb6cYdvULKBSzw2FBs+2DGpSF89PjXrSfY=" }
  }
  override_data {
    target = data.aws_dynamodb_table.verify_zero_etl[0]
    values = {
      arn                    = "arn:aws:dynamodb:eu-central-1:222222222222:table/example-zero-etl-source", billing_mode = "PAY_PER_REQUEST", hash_key = "order_id"
      attribute              = [{ name = "order_id", type = "S" }]
      point_in_time_recovery = [{ enabled = true, recovery_period_in_days = 35 }]
    }
  }
  override_data {
    target = data.aws_s3_bucket_policy.verify_fixture
    values = {
      policy = jsonencode({
        Version = "2012-10-17"
        Statement = [
          {
            Sid       = "RequireTLS", Effect = "Deny", Principal = "*", Action = "s3:*"
            Resource  = ["arn:aws:s3:::mock-fixture", "arn:aws:s3:::mock-fixture/*"]
            Condition = { Bool = { "aws:SecureTransport" = "false" } }
          },
          {
            Sid      = "RedshiftEventIntegration", Effect = "Allow", Principal = { Service = "redshift.amazonaws.com" }
            Action   = ["s3:GetBucketNotification", "s3:PutBucketNotification", "s3:GetBucketLocation"]
            Resource = ["arn:aws:s3:::mock-fixture"]
            Condition = {
              StringEquals = { "aws:SourceAccount" = "111111111111" }
              ArnLike      = { "aws:SourceArn" = "arn:aws:redshift:eu-central-1:111111111111:integration:*" }
            }
          },
          {
            Sid    = "ConsumerBucketMetadata", Effect = "Allow", Principal = { AWS = "arn:aws:iam::222222222222:role/consumer-fixture" }
            Action = ["s3:GetBucketLocation", "s3:ListBucket"], Resource = ["arn:aws:s3:::mock-fixture"]
          },
          {
            Sid    = "ConsumerFixtureRead", Effect = "Allow", Principal = { AWS = "arn:aws:iam::222222222222:role/consumer-fixture" }
            Action = ["s3:GetObject"], Resource = ["arn:aws:s3:::mock-fixture/data/*", "arn:aws:s3:::mock-fixture/unload/*"]
          },
          {
            Sid    = "ConsumerUnload", Effect = "Allow", Principal = { AWS = "arn:aws:iam::222222222222:role/consumer-fixture" }
            Action = ["s3:PutObject", "s3:AbortMultipartUpload"], Resource = ["arn:aws:s3:::mock-fixture/unload/*"]
          },
        ]
      })
    }
  }
  assert {
    condition = (
      redshift_datashare_grant.consumer.account_id == "222222222222" && redshift_datashare_grant.consumer.namespace_id == null &&
      length(aws_redshift_data_share_authorization.consumer) == 1 && length(aws_redshift_data_share_consumer_association.this) == 1 &&
      aws_redshift_data_share_authorization.consumer[0].consumer_identifier == "222222222222" &&
      aws_redshift_data_share_authorization.consumer[0].data_share_arn == local.datashare_arn &&
      aws_redshift_data_share_consumer_association.this[0].data_share_arn == local.datashare_arn &&
      aws_redshift_data_share_consumer_association.this[0].consumer_arn == aws_redshiftserverless_namespace.consumer.arn &&
      !aws_redshift_data_share_consumer_association.this[0].allow_writes && redshift_database.shared.datashare_arn == local.datashare_arn &&
      length(aws_glue_resource_policy.fixture) == 1 &&
      jsondecode(aws_glue_resource_policy.fixture[0].policy).Statement[0].Principal.AWS == aws_iam_role.consumer.arn &&
      toset([for s in jsondecode(local.fixture_bucket_policy).Statement : s.Sid]) == toset([
        "RequireTLS", "ConsumerBucketMetadata", "ConsumerFixtureRead", "ConsumerUnload", "RedshiftEventIntegration",
      ]) &&
      alltrue([for s in jsondecode(local.fixture_bucket_policy).Statement :
        s.Sid == "RedshiftEventIntegration" ? (
          s.Principal.Service == "redshift.amazonaws.com" &&
          s.Condition.StringEquals["aws:SourceAccount"] == data.aws_caller_identity.producer.account_id &&
          s.Condition.ArnLike["aws:SourceArn"] == "arn:aws:redshift:${var.region}:${data.aws_caller_identity.producer.account_id}:integration:*"
        ) : s.Principal.AWS == aws_iam_role.consumer.arn if s.Effect == "Allow"
      ]) &&
      jsondecode(aws_iam_role.consumer.assume_role_policy).Statement[0].Condition.StringEquals["aws:SourceAccount"] == "222222222222"
    )
    error_message = "A different caller account must select an account-only SQL grant, both AWS sharing controls, and cross-account fixture access."
  }
}

run "all_connection_checks" {
  command   = apply
  state_key = "connections"
  variables {
    connection_checks = [
      "producer_data_api_iam", "consumer_data_api_iam", "producer_direct_iam",
      "consumer_direct_iam", "producer_direct_password", "consumer_direct_password",
    ]
    producer_password = "MockOnlyProducerPassword123"
    consumer_password = "MockOnlyConsumerPassword123"
    allow_public_sql  = true
    public_sql_cidrs  = ["203.0.113.10/32"]
  }
  override_data {
    target = data.aws_route_table.verify_producer
    values = {
      vpc_id = "vpc-producer"
      routes = [
        { cidr_block = "10.42.0.0/16", gateway_id = "local", ipv6_cidr_block = "", destination_prefix_list_id = "" },
        { cidr_block = "", gateway_id = "vpce-producer-s3", ipv6_cidr_block = "", destination_prefix_list_id = "pl-producer-s3" },
        { cidr_block = "0.0.0.0/0", gateway_id = "igw-producer", ipv6_cidr_block = "", destination_prefix_list_id = "" },
      ]
    }
  }
  override_data {
    target = data.aws_route_table.verify_consumer
    values = {
      vpc_id = "vpc-consumer"
      routes = [
        { cidr_block = "10.43.0.0/16", gateway_id = "local", ipv6_cidr_block = "", destination_prefix_list_id = "" },
        { cidr_block = "", gateway_id = "vpce-consumer-s3", ipv6_cidr_block = "", destination_prefix_list_id = "pl-consumer-s3" },
        { cidr_block = "0.0.0.0/0", gateway_id = "igw-consumer", ipv6_cidr_block = "", destination_prefix_list_id = "" },
      ]
    }
  }
  override_data {
    target = data.aws_vpc_endpoint.verify_producer["s3"]
    values = {
      state           = "available", vpc_id = "vpc-producer", service_name = "com.amazonaws.eu-central-1.s3", vpc_endpoint_type = "Gateway"
      route_table_ids = ["rtb-producer-public"]
    }
  }
  override_data {
    target = data.aws_vpc_endpoint.verify_consumer["s3"]
    values = {
      state           = "available", vpc_id = "vpc-consumer", service_name = "com.amazonaws.eu-central-1.s3", vpc_endpoint_type = "Gateway"
      route_table_ids = ["rtb-consumer-public"]
    }
  }
  override_data {
    target = data.aws_vpc_security_group_rules.verify_producer
    values = { ids = ["sgr-producer-vpc", "sgr-producer-s3", "sgr-producer-client"] }
  }
  override_data {
    target = data.aws_vpc_security_group_rules.verify_consumer
    values = { ids = ["sgr-consumer-vpc", "sgr-consumer-s3", "sgr-consumer-client"] }
  }
  override_data {
    target = data.aws_redshift_cluster.verify_producer
    values = {
      encrypted                    = true, enhanced_vpc_routing = true, publicly_accessible = true, vpc_id = "vpc-producer"
      node_type                    = "ra3.large", database_name = "dev", master_username = "tfadmin", port = 5439
      cluster_subnet_group_name    = "redshift-oss-e2e-deadbeef-producer"
      cluster_parameter_group_name = "redshift-oss-e2e-deadbeef-producer"
      vpc_security_group_ids       = ["sg-producer"]
      iam_roles                    = ["arn:aws:iam::111111111111:role/producer-fixture"]
      default_iam_role_arn         = "arn:aws:iam::111111111111:role/producer-fixture"
      cluster_namespace_arn        = "arn:aws:redshift:eu-central-1:111111111111:namespace:11111111-2222-3333-4444-555555555555"
    }
  }
  override_data {
    target = data.aws_redshiftserverless_workgroup.verify_consumer
    values = {
      namespace_name     = "redshift-oss-e2e-deadbeef-consumer", enhanced_vpc_routing = true, publicly_accessible = true
      subnet_ids         = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
      security_group_ids = ["sg-consumer"]
      endpoint           = [{ address = "consumer.example.test", port = 5439 }]
    }
  }
  override_data {
    target = data.redshift_datashare.producer
    values = { publicly_accessible = true }
  }
  assert {
    condition = (
      length(data.redshift_database.producer_data_api_iam) == 1 && length(data.redshift_database.consumer_data_api_iam) == 1 &&
      length(data.redshift_database.producer_direct_iam) == 1 && length(data.redshift_database.consumer_direct_iam) == 1 &&
      length(data.redshift_database.producer_direct_password) == 1 && length(data.redshift_database.consumer_direct_password) == 1 &&
      redshift_datashare.producer.publicly_accessible && output.producer_share.publicly_accessible &&
      toset(keys(output.connection_checks)) == var.connection_checks &&
      alltrue([for name in values(output.connection_checks) : name == var.admin_database]) &&
      aws_redshift_cluster.producer.publicly_accessible && aws_redshiftserverless_workgroup.consumer.publicly_accessible &&
      module.producer_vpc.igw_id != null && module.consumer_vpc.igw_id != null &&
      length(module.producer_vpc.public_subnets) == 3 && length(module.consumer_vpc.public_subnets) == 3 &&
      length(module.producer_vpc.intra_subnets) == 0 && length(module.consumer_vpc.intra_subnets) == 0 &&
      length(module.producer_vpc.public_route_table_association_ids) == 3 && length(module.consumer_vpc.public_route_table_association_ids) == 3 &&
      toset(aws_redshift_subnet_group.producer.subnet_ids) == toset(module.producer_vpc.public_subnets) &&
      toset(aws_redshiftserverless_workgroup.consumer.subnet_ids) == toset(module.consumer_vpc.public_subnets) &&
      toset(module.producer_endpoints.endpoints["s3"].route_table_ids) == toset(module.producer_vpc.public_route_table_ids) &&
      toset(module.consumer_endpoints.endpoints["s3"].route_table_ids) == toset(module.consumer_vpc.public_route_table_ids) &&
      toset(keys(aws_vpc_security_group_ingress_rule.producer_sql)) == toset(var.public_sql_cidrs) &&
      toset(keys(aws_vpc_security_group_ingress_rule.consumer_sql)) == toset(var.public_sql_cidrs) &&
      alltrue([for rule in concat(values(aws_vpc_security_group_ingress_rule.producer_sql), values(aws_vpc_security_group_ingress_rule.consumer_sql)) :
        rule.from_port == 5439 && rule.to_port == 5439 && rule.ip_protocol == "tcp" && contains(var.public_sql_cidrs, rule.cidr_ipv4)
      ])
    )
    error_message = "All six optional read-only aliases must expose the created administration database when direct connectivity is opted in."
  }
}

# A plan-only run covers the initial plan without mocked applies.
run "plan_defaults" {
  command = plan
  assert {
    condition     = redshift_role_grant.operators.role == "sys:operator" && redshift_user.loader.create_database
    error_message = "The default plan must include the least-privilege operator role and the loader identity."
  }
}

run "rejects_unrestricted_public_cidr" {
  command = plan
  variables { public_sql_cidrs = ["0.0.0.0/0"] }
  expect_failures = [var.public_sql_cidrs]
}

run "rejects_public_sql_without_cidrs" {
  command = plan
  variables { allow_public_sql = true }
  expect_failures = [var.allow_public_sql]
}

run "rejects_reserved_admin_database" {
  command = plan
  variables { admin_database = "example_local" }
  expect_failures = [var.admin_database]
}

run "rejects_unknown_connection_check" {
  command = plan
  variables { connection_checks = ["consumer_odbc"] }
  expect_failures = [var.connection_checks]
}

run "rejects_invalid_identity_center_arn" {
  command = plan
  variables { identity_center_instance_arn = "arn:aws:sso:::application/invalid" }
  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }
  expect_failures = [var.identity_center_instance_arn]
}

run "rejects_invalid_name_prefix" {
  command = plan
  variables { name_prefix = "Invalid_Prefix" }
  expect_failures = [var.name_prefix]
}

run "rejects_password_probe_without_password" {
  command = plan
  variables { connection_checks = ["consumer_direct_password"] }
  expect_failures = [var.consumer_password]
}

run "rejects_wrong_identity_center_binding" {
  command   = apply
  state_key = "wrong_identity_center_binding"
  variables {
    identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
  }
  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }
  override_data {
    target = data.redshift_identity_provider.this[0]
    values = {
      type            = "AWSIDC"
      enabled         = true
      namespace       = "unrelated_namespace"
      application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef"
      iam_role_arn    = "arn:aws:iam::111111111111:role/consumer-fixture"
    }
  }

  expect_failures = [data.redshift_identity_provider.this]
}

run "rejects_missing_identity_center_role_grant" {
  command   = apply
  state_key = "missing_identity_center_grant"
  variables {
    identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
  }
  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }
  override_data {
    target = data.redshift_role_grant.sso_readers[0]
    values = { exists = false, admin_option = false }
  }

  expect_failures = [data.redshift_role_grant.sso_readers]
}
