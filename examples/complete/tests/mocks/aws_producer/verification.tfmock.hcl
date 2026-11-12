# Service observations for the default private/same-account topology. Public/cross-account runs override the
# affected observations explicitly; assertions are never disabled for mocks. Stable resource IDs connect the
# independently supplied observations to the managed topology without depending on Terraform's random mock IDs.
mock_resource "aws_vpc" {
  defaults = { id = "vpc-producer" }
}
mock_resource "aws_security_group" {
  defaults = { id = "sg-producer-endpoints" }
}
override_resource {
  target = aws_security_group.producer
  values = { id = "sg-producer" }
}
override_resource {
  target = module.producer_vpc.aws_subnet.intra[0]
  values = { id = "subnet-producer-0" }
}
override_resource {
  target = module.producer_vpc.aws_subnet.intra[1]
  values = { id = "subnet-producer-1" }
}
override_resource {
  target = module.producer_vpc.aws_subnet.intra[2]
  values = { id = "subnet-producer-2" }
}
override_resource {
  target = module.producer_vpc.aws_subnet.public[0]
  values = { id = "subnet-producer-0" }
}
override_resource {
  target = module.producer_vpc.aws_subnet.public[1]
  values = { id = "subnet-producer-1" }
}
override_resource {
  target = module.producer_vpc.aws_subnet.public[2]
  values = { id = "subnet-producer-2" }
}
override_resource {
  target = module.producer_vpc.aws_route_table.intra[0]
  values = { id = "rtb-producer-0" }
}
override_resource {
  target = module.producer_vpc.aws_route_table.public[0]
  values = { id = "rtb-producer-public" }
}
mock_resource "aws_internet_gateway" {
  defaults = { id = "igw-producer" }
}
override_resource {
  target = module.producer_endpoints.aws_vpc_endpoint.this["s3"]
  values = { id = "vpce-producer-s3", prefix_list_id = "pl-producer-s3" }
}
override_resource {
  target = module.producer_endpoints.aws_vpc_endpoint.this["glue"]
  values = { id = "vpce-producer-glue" }
}
override_data {
  target = module.producer_endpoints.data.aws_vpc_endpoint_service.this["s3"]
  values = { service_name = "com.amazonaws.eu-central-1.s3" }
}
override_data {
  target = module.producer_endpoints.data.aws_vpc_endpoint_service.this["glue"]
  values = { service_name = "com.amazonaws.eu-central-1.glue" }
}

mock_data "aws_vpc" {
  defaults = {
    cidr_block           = "10.42.0.0/16"
    owner_id             = "111111111111"
    enable_dns_support   = true
    enable_dns_hostnames = true
  }
}
override_data {
  target = data.aws_subnet.verify_producer["0"]
  values = { vpc_id = "vpc-producer", cidr_block = "10.42.0.0/24", availability_zone = "eu-central-1a", owner_id = "111111111111", map_public_ip_on_launch = false }
}
override_data {
  target = data.aws_subnet.verify_producer["1"]
  values = { vpc_id = "vpc-producer", cidr_block = "10.42.1.0/24", availability_zone = "eu-central-1b", owner_id = "111111111111", map_public_ip_on_launch = false }
}
override_data {
  target = data.aws_subnet.verify_producer["2"]
  values = { vpc_id = "vpc-producer", cidr_block = "10.42.2.0/24", availability_zone = "eu-central-1c", owner_id = "111111111111", map_public_ip_on_launch = false }
}
mock_data "aws_route_table" {
  defaults = {
    vpc_id = "vpc-producer"
    routes = [
      { cidr_block = "10.42.0.0/16", gateway_id = "local", ipv6_cidr_block = "", destination_prefix_list_id = "" },
      { cidr_block = "", gateway_id = "vpce-producer-s3", ipv6_cidr_block = "", destination_prefix_list_id = "pl-producer-s3" },
    ]
  }
}
override_data {
  target = data.aws_vpc_endpoint.verify_producer["s3"]
  values = {
    state             = "available"
    vpc_id            = "vpc-producer"
    service_name      = "com.amazonaws.eu-central-1.s3"
    vpc_endpoint_type = "Gateway"
    route_table_ids   = ["rtb-producer-0"]
  }
}
override_data {
  target = data.aws_vpc_endpoint.verify_producer["glue"]
  values = {
    state               = "available"
    vpc_id              = "vpc-producer"
    service_name        = "com.amazonaws.eu-central-1.glue"
    vpc_endpoint_type   = "Interface"
    private_dns_enabled = true
    subnet_ids          = ["subnet-producer-0", "subnet-producer-1", "subnet-producer-2"]
    security_group_ids  = ["sg-producer-endpoints"]
  }
}
mock_data "aws_vpc_security_group_rules" {
  defaults = { ids = ["sgr-producer-vpc", "sgr-producer-s3"] }
}
mock_data "aws_vpc_security_group_rule" {
  defaults = { is_egress = false, security_group_id = "sg-producer", ip_protocol = "tcp", from_port = 5439, to_port = 5439, cidr_ipv4 = "203.0.113.10/32" }
}
override_data {
  target = data.aws_vpc_security_group_rule.verify_producer_egress["vpc"]
  values = { is_egress = true, security_group_id = "sg-producer", ip_protocol = "-1", cidr_ipv4 = "10.42.0.0/16" }
}
override_data {
  target = data.aws_vpc_security_group_rule.verify_producer_egress["s3"]
  values = { is_egress = true, security_group_id = "sg-producer", ip_protocol = "tcp", from_port = 443, to_port = 443, prefix_list_id = "pl-producer-s3" }
}
mock_data "aws_redshift_cluster" {
  defaults = {
    encrypted                    = true
    enhanced_vpc_routing         = true
    publicly_accessible          = false
    vpc_id                       = "vpc-producer"
    node_type                    = "ra3.large"
    database_name                = "dev"
    master_username              = "tfadmin"
    port                         = 5439
    cluster_subnet_group_name    = "redshift-oss-e2e-deadbeef-producer"
    cluster_parameter_group_name = "redshift-oss-e2e-deadbeef-producer"
    vpc_security_group_ids       = ["sg-producer"]
    iam_roles                    = ["arn:aws:iam::111111111111:role/producer-fixture"]
    default_iam_role_arn         = "arn:aws:iam::111111111111:role/producer-fixture"
    cluster_namespace_arn        = "arn:aws:redshift:eu-central-1:111111111111:namespace:11111111-2222-3333-4444-555555555555"
  }
}
mock_data "aws_redshift_subnet_group" {
  defaults = { subnet_ids = ["subnet-producer-0", "subnet-producer-1", "subnet-producer-2"] }
}
override_data {
  target = data.aws_iam_role.verify_producer
  values = {
    arn = "arn:aws:iam::111111111111:role/producer-fixture"
    assume_role_policy = jsonencode({
      Version = "2012-10-17"
      Statement = [{
        Effect    = "Allow"
        Principal = { Service = "redshift.amazonaws.com" }
        Action    = "sts:AssumeRole"
        Condition = { StringEquals = { "aws:SourceAccount" = "111111111111" } }
      }]
    })
  }
}
mock_data "aws_s3_bucket" {
  defaults = { arn = "arn:aws:s3:::mock-fixture", bucket_region = "eu-central-1" }
}
mock_data "aws_iam_role_policies" {
  defaults = { policy_names = ["fixture", "spectrum-tables"] }
}
mock_data "aws_s3_bucket_policy" {
  defaults = {
    policy = jsonencode({
      Version = "2012-10-17"
      Statement = [{
        Sid       = "RequireTLS"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = ["arn:aws:s3:::mock-fixture", "arn:aws:s3:::mock-fixture/*"]
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
        }, {
        Sid       = "RedshiftEventIntegration"
        Effect    = "Allow"
        Principal = { Service = "redshift.amazonaws.com" }
        Action    = ["s3:GetBucketNotification", "s3:PutBucketNotification", "s3:GetBucketLocation"]
        Resource  = ["arn:aws:s3:::mock-fixture"]
        Condition = {
          StringEquals = { "aws:SourceAccount" = "111111111111" }
          ArnLike      = { "aws:SourceArn" = "arn:aws:redshift:eu-central-1:111111111111:integration:*" }
        }
      }]
    })
  }
}
mock_data "aws_s3_object" {
  defaults = { body = "id,label\n1,first\n2,second\n", content_type = "text/csv", server_side_encryption = "AES256" }
}
mock_data "aws_glue_catalog_table" {
  defaults = {
    table_type = "EXTERNAL_TABLE"
    parameters = { "skip.header.line.count" = "1" }
    storage_descriptor = [{
      location      = "s3://mock-fixture/data/"
      input_format  = "org.apache.hadoop.mapred.TextInputFormat"
      output_format = "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat"
      ser_de_info = [{
        serialization_library = "org.apache.hadoop.hive.serde2.lazy.LazySimpleSerDe"
        parameters            = { "field.delim" = "," }
      }]
      columns = [{ name = "id", type = "int" }, { name = "label", type = "varchar(64)" }]
    }]
  }
}
