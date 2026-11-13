# Default private/same-account service readbacks. See the producer fixture for the stable-ID convention.
mock_resource "aws_vpc" {
  defaults = { id = "vpc-consumer" }
}
mock_resource "aws_security_group" {
  defaults = { id = "sg-consumer-endpoints" }
}
override_resource {
  target = aws_security_group.consumer
  values = { id = "sg-consumer" }
}
override_resource {
  target = module.consumer_vpc.aws_subnet.intra[0]
  values = { id = "subnet-consumer-0" }
}
override_resource {
  target = module.consumer_vpc.aws_subnet.intra[1]
  values = { id = "subnet-consumer-1" }
}
override_resource {
  target = module.consumer_vpc.aws_subnet.intra[2]
  values = { id = "subnet-consumer-2" }
}
override_resource {
  target = module.consumer_vpc.aws_subnet.public[0]
  values = { id = "subnet-consumer-0" }
}
override_resource {
  target = module.consumer_vpc.aws_subnet.public[1]
  values = { id = "subnet-consumer-1" }
}
override_resource {
  target = module.consumer_vpc.aws_subnet.public[2]
  values = { id = "subnet-consumer-2" }
}
override_resource {
  target = module.consumer_vpc.aws_route_table.intra[0]
  values = { id = "rtb-consumer-0" }
}
override_resource {
  target = module.consumer_vpc.aws_route_table.public[0]
  values = { id = "rtb-consumer-public" }
}
mock_resource "aws_internet_gateway" {
  defaults = { id = "igw-consumer" }
}
override_resource {
  target = module.consumer_endpoints.aws_vpc_endpoint.this["s3"]
  values = { id = "vpce-consumer-s3", prefix_list_id = "pl-consumer-s3" }
}
override_resource {
  target = module.consumer_endpoints.aws_vpc_endpoint.this["glue"]
  values = { id = "vpce-consumer-glue" }
}
override_resource {
  target = module.consumer_endpoints.aws_vpc_endpoint.this["sso_oauth"]
  values = { id = "vpce-consumer-sso-oauth" }
}
override_resource {
  target = module.consumer_endpoints.aws_vpc_endpoint.this["identitystore"]
  values = { id = "vpce-consumer-identitystore" }
}
override_data {
  target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["s3"]
  values = { service_name = "com.amazonaws.eu-central-1.s3" }
}
override_data {
  target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["glue"]
  values = { service_name = "com.amazonaws.eu-central-1.glue" }
}
override_data {
  target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["sso_oauth"]
  values = { service_name = "com.amazonaws.eu-central-1.sso-oauth" }
}
override_data {
  target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["identitystore"]
  values = { service_name = "com.amazonaws.eu-central-1.identitystore" }
}

mock_resource "aws_rds_cluster" {
  defaults = {
    id  = "redshift-oss-e2e-deadbeef-zero-etl-source"
    arn = "arn:aws:rds:eu-central-1:111111111111:cluster:redshift-oss-e2e-deadbeef-zero-etl-source"
  }
}
mock_resource "aws_rds_cluster_instance" {
  defaults = { id = "redshift-oss-e2e-deadbeef-zero-etl-source" }
}

mock_data "aws_vpc" {
  defaults = {
    cidr_block           = "10.43.0.0/16"
    owner_id             = "111111111111"
    enable_dns_support   = true
    enable_dns_hostnames = true
  }
}
override_data {
  target = data.aws_subnet.verify_consumer["0"]
  values = { vpc_id = "vpc-consumer", cidr_block = "10.43.0.0/24", availability_zone = "eu-central-1a", owner_id = "111111111111", map_public_ip_on_launch = false }
}
override_data {
  target = data.aws_subnet.verify_consumer["1"]
  values = { vpc_id = "vpc-consumer", cidr_block = "10.43.1.0/24", availability_zone = "eu-central-1b", owner_id = "111111111111", map_public_ip_on_launch = false }
}
override_data {
  target = data.aws_subnet.verify_consumer["2"]
  values = { vpc_id = "vpc-consumer", cidr_block = "10.43.2.0/24", availability_zone = "eu-central-1c", owner_id = "111111111111", map_public_ip_on_launch = false }
}
mock_data "aws_route_table" {
  defaults = {
    vpc_id = "vpc-consumer"
    routes = [
      { cidr_block = "10.43.0.0/16", gateway_id = "local", ipv6_cidr_block = "", destination_prefix_list_id = "" },
      { cidr_block = "", gateway_id = "vpce-consumer-s3", ipv6_cidr_block = "", destination_prefix_list_id = "pl-consumer-s3" },
    ]
  }
}
override_data {
  target = data.aws_vpc_endpoint.verify_consumer["s3"]
  values = {
    state             = "available"
    vpc_id            = "vpc-consumer"
    service_name      = "com.amazonaws.eu-central-1.s3"
    vpc_endpoint_type = "Gateway"
    route_table_ids   = ["rtb-consumer-0"]
  }
}
override_data {
  target = data.aws_vpc_endpoint.verify_consumer["glue"]
  values = {
    state               = "available"
    vpc_id              = "vpc-consumer"
    service_name        = "com.amazonaws.eu-central-1.glue"
    vpc_endpoint_type   = "Interface"
    private_dns_enabled = true
    subnet_ids          = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
    security_group_ids  = ["sg-consumer-endpoints"]
  }
}
override_data {
  target = data.aws_vpc_endpoint.verify_consumer["sso_oauth"]
  values = {
    state               = "available"
    vpc_id              = "vpc-consumer"
    service_name        = "com.amazonaws.eu-central-1.sso-oauth"
    vpc_endpoint_type   = "Interface"
    private_dns_enabled = true
    subnet_ids          = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
    security_group_ids  = ["sg-consumer-endpoints"]
  }
}
override_data {
  target = data.aws_vpc_endpoint.verify_consumer["identitystore"]
  values = {
    state               = "available"
    vpc_id              = "vpc-consumer"
    service_name        = "com.amazonaws.eu-central-1.identitystore"
    vpc_endpoint_type   = "Interface"
    private_dns_enabled = true
    subnet_ids          = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
    security_group_ids  = ["sg-consumer-endpoints"]
  }
}
mock_data "aws_vpc_security_group_rules" {
  defaults = { ids = ["sgr-consumer-vpc", "sgr-consumer-s3"] }
}
mock_data "aws_vpc_security_group_rule" {
  defaults = { is_egress = false, security_group_id = "sg-consumer", ip_protocol = "tcp", from_port = 5439, to_port = 5439, cidr_ipv4 = "203.0.113.10/32" }
}
override_data {
  target = data.aws_vpc_security_group_rule.verify_consumer_egress["vpc"]
  values = { is_egress = true, security_group_id = "sg-consumer", ip_protocol = "-1", cidr_ipv4 = "10.43.0.0/16" }
}
override_data {
  target = data.aws_vpc_security_group_rule.verify_consumer_egress["s3"]
  values = { is_egress = true, security_group_id = "sg-consumer", ip_protocol = "tcp", from_port = 443, to_port = 443, prefix_list_id = "pl-consumer-s3" }
}
mock_data "aws_redshiftserverless_namespace" {
  defaults = {
    arn                  = "arn:aws:redshift-serverless:eu-central-1:111111111111:namespace/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    namespace_id         = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    db_name              = "dev"
    admin_username       = "tfadmin"
    default_iam_role_arn = "arn:aws:iam::111111111111:role/consumer-fixture"
    iam_roles            = ["arn:aws:iam::111111111111:role/consumer-fixture"]
  }
}
mock_data "aws_redshiftserverless_workgroup" {
  defaults = {
    namespace_name       = "redshift-oss-e2e-deadbeef-consumer"
    enhanced_vpc_routing = true
    publicly_accessible  = false
    subnet_ids           = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
    security_group_ids   = ["sg-consumer"]
    endpoint             = [{ address = "consumer.example.test", port = 5439 }]
  }
}
override_data {
  target = data.aws_iam_role.verify_consumer
  values = {
    arn = "arn:aws:iam::111111111111:role/consumer-fixture"
    assume_role_policy = jsonencode({
      Version = "2012-10-17"
      Statement = [{
        Effect    = "Allow"
        Principal = { Service = ["redshift.amazonaws.com", "redshift-serverless.amazonaws.com"] }
        Action    = ["sts:AssumeRole", "sts:SetContext"]
        Condition = { StringEquals = { "aws:SourceAccount" = "111111111111" } }
      }]
    })
  }
}
override_resource {
  target = aws_iam_role.extfunctions_lambda
  values = { id = "redshift-oss-e2e-deadbeef-udf-exec", arn = "arn:aws:iam::111111111111:role/lambda-udf-exec" }
}
override_data {
  target = data.aws_iam_role.verify_lambda
  values = {
    arn = "arn:aws:iam::111111111111:role/lambda-udf-exec"
    assume_role_policy = jsonencode({
      Version   = "2012-10-17"
      Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole" }]
    })
  }
}
mock_data "aws_lambda_function" {
  defaults = {
    role        = "arn:aws:iam::111111111111:role/lambda-udf-exec"
    runtime     = "python3.12"
    handler     = "index.handler"
    code_sha256 = "uQKxXeOixWb6cYdvULKBSzw2FBs+2DGpSF89PjXrSfY="
  }
}
mock_resource "aws_lambda_function" {
  defaults = { arn = "arn:aws:lambda:eu-central-1:111111111111:function:redshift-oss-e2e-deadbeef-upper" }
}
mock_data "aws_iam_role_policies" {
  defaults = { policy_names = ["fixture", "lambda-udf"] }
}
mock_data "aws_iam_role_policy_attachments" {
  defaults = { attached_policies = [{ policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole", policy_name = "AWSLambdaBasicExecutionRole" }] }
}
mock_data "aws_secretsmanager_secret_version" {
  defaults = {
    version_stages = ["AWSCURRENT"]
    secret_string  = "{\"username\":\"example_reader\",\"password\":\"MockOnlyPassword123456789\"}"
  }
}
mock_data "aws_dynamodb_table" {
  defaults = {
    arn                    = "arn:aws:dynamodb:eu-central-1:111111111111:table/example-zero-etl-source"
    billing_mode           = "PAY_PER_REQUEST"
    hash_key               = "order_id"
    attribute              = [{ name = "order_id", type = "S" }]
    point_in_time_recovery = [{ enabled = true, recovery_period_in_days = 35 }]
  }
}
mock_data "aws_rds_cluster" {
  defaults = {
    engine                          = "aurora-postgresql"
    storage_encrypted               = true
    database_name                   = "orders"
    db_cluster_parameter_group_name = "redshift-oss-e2e-deadbeef-zero-etl-source"
    db_subnet_group_name            = "redshift-oss-e2e-deadbeef-zero-etl-source"
    vpc_security_group_ids          = ["sg-consumer"]
    cluster_members                 = ["redshift-oss-e2e-deadbeef-zero-etl-source"]
  }
}
mock_data "aws_rds_engine_version" {
  defaults = { version_actual = "16.4" }
}
mock_data "aws_db_instance" {
  defaults = {
    db_cluster_identifier = "redshift-oss-e2e-deadbeef-zero-etl-source"
    engine                = "aurora-postgresql"
    db_instance_class     = "db.serverless"
    storage_encrypted     = true
    publicly_accessible   = false
  }
}
mock_data "aws_ssoadmin_application" {
  defaults = { instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef", status = "ENABLED" }
}
override_resource {
  target = aws_identitystore_group.readers[0]
  values = { group_id = "11111111-1111-1111-1111-111111111111" }
}
override_resource {
  target = aws_identitystore_group.operators[0]
  values = { group_id = "22222222-2222-2222-2222-222222222222" }
}
override_data {
  target = data.aws_identitystore_group.verify_consumer["readers"]
  values = { display_name = "redshift-oss-e2e-deadbeef-readers" }
}
override_data {
  target = data.aws_identitystore_group.verify_consumer["operators"]
  values = { display_name = "redshift-oss-e2e-deadbeef-operators" }
}
# Runs selecting existing directory groups must supply their corresponding observed group names and assignments.
mock_data "aws_ssoadmin_application_assignments" {
  defaults = {
    application_assignments = [
      { principal_type = "GROUP", principal_id = "11111111-1111-1111-1111-111111111111", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
      { principal_type = "GROUP", principal_id = "22222222-2222-2222-2222-222222222222", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
    ]
  }
}
