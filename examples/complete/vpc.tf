data "aws_availability_zones" "producer" {
  provider = aws.producer
  state    = "available"
  filter {
    name   = "zone-type"
    values = ["availability-zone"]
  }
}

data "aws_availability_zones" "consumer" {
  provider = aws.consumer
  state    = "available"
  filter {
    name   = "zone-type"
    values = ["availability-zone"]
  }
}

locals {
  producer_subnet_cidrs = [for i in range(3) : cidrsubnet(var.producer_vpc_cidr, 8, i)]
  consumer_subnet_cidrs = [for i in range(3) : cidrsubnet(var.consumer_vpc_cidr, 8, i)]
  producer_subnet_ids   = concat(module.producer_vpc.intra_subnets, module.producer_vpc.public_subnets)
  consumer_subnet_ids   = concat(module.consumer_vpc.intra_subnets, module.consumer_vpc.public_subnets)
}

# Separate networks support different AWS accounts. No NAT or peering is needed by the Data API.
# Disposable example networks omit flow-log infrastructure.
# trivy:ignore:AWS-0178
module "producer_vpc" {
  source    = "terraform-aws-modules/vpc/aws"
  version   = "6.6.1"
  providers = { aws = aws.producer }

  name = "${local.name}-producer"
  cidr = var.producer_vpc_cidr
  azs  = slice(data.aws_availability_zones.producer.names, 0, 3)

  intra_subnets                 = var.allow_public_sql ? [] : local.producer_subnet_cidrs
  public_subnets                = var.allow_public_sql ? local.producer_subnet_cidrs : []
  manage_default_network_acl    = false
  manage_default_route_table    = false
  manage_default_security_group = false
}

# trivy:ignore:AWS-0178
module "consumer_vpc" {
  source    = "terraform-aws-modules/vpc/aws"
  version   = "6.6.1"
  providers = { aws = aws.consumer }

  name = "${local.name}-consumer"
  cidr = var.consumer_vpc_cidr
  azs  = slice(data.aws_availability_zones.consumer.names, 0, 3)

  intra_subnets                 = var.allow_public_sql ? [] : local.consumer_subnet_cidrs
  public_subnets                = var.allow_public_sql ? local.consumer_subnet_cidrs : []
  manage_default_network_acl    = false
  manage_default_route_table    = false
  manage_default_security_group = false
}

resource "aws_security_group" "producer" {
  provider    = aws.producer
  name        = "${local.name}-producer"
  description = "Producer SQL access and private fixture endpoints"
  vpc_id      = module.producer_vpc.vpc_id
}

resource "aws_security_group" "consumer" {
  provider    = aws.consumer
  name        = "${local.name}-consumer"
  description = "Consumer SQL access and private fixture endpoints"
  vpc_id      = module.consumer_vpc.vpc_id
}

resource "aws_vpc_security_group_ingress_rule" "producer_sql" {
  provider          = aws.producer
  for_each          = var.allow_public_sql ? toset(var.public_sql_cidrs) : toset([])
  security_group_id = aws_security_group.producer.id
  description       = "Explicitly allowed SQL client"
  from_port         = 5439
  to_port           = 5439
  ip_protocol       = "tcp"
  cidr_ipv4         = each.value
}

resource "aws_vpc_security_group_ingress_rule" "consumer_sql" {
  provider          = aws.consumer
  for_each          = var.allow_public_sql ? toset(var.public_sql_cidrs) : toset([])
  security_group_id = aws_security_group.consumer.id
  description       = "Explicitly allowed SQL client"
  from_port         = 5439
  to_port           = 5439
  ip_protocol       = "tcp"
  cidr_ipv4         = each.value
}

resource "aws_vpc_security_group_egress_rule" "producer_vpc" {
  provider          = aws.producer
  security_group_id = aws_security_group.producer.id
  description       = "Warehouse and interface endpoints within the VPC"
  ip_protocol       = "-1"
  cidr_ipv4         = module.producer_vpc.vpc_cidr_block
}

resource "aws_vpc_security_group_egress_rule" "consumer_vpc" {
  provider          = aws.consumer
  security_group_id = aws_security_group.consumer.id
  description       = "Warehouse and interface endpoints within the VPC"
  ip_protocol       = "-1"
  cidr_ipv4         = module.consumer_vpc.vpc_cidr_block
}

# Separate egress rules avoid a dependency cycle between warehouse groups and endpoint modules.
resource "aws_vpc_security_group_egress_rule" "producer_s3" {
  provider          = aws.producer
  security_group_id = aws_security_group.producer.id
  description       = "S3 through the gateway endpoint"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  prefix_list_id    = module.producer_endpoints.endpoints["s3"].prefix_list_id
}

resource "aws_vpc_security_group_egress_rule" "consumer_s3" {
  provider          = aws.consumer
  security_group_id = aws_security_group.consumer.id
  description       = "S3 through the gateway endpoint"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  prefix_list_id    = module.consumer_endpoints.endpoints["s3"].prefix_list_id
}

module "producer_endpoints" {
  source    = "terraform-aws-modules/vpc/aws//modules/vpc-endpoints"
  version   = "6.6.1"
  providers = { aws = aws.producer }

  vpc_id                     = module.producer_vpc.vpc_id
  subnet_ids                 = local.producer_subnet_ids
  create_security_group      = true
  security_group_name_prefix = "${local.name}-producer-endpoints-"
  security_group_description = "HTTPS from producer warehouse to private endpoints"
  security_group_rules = {
    warehouse_https = {
      description              = "Producer warehouse HTTPS"
      source_security_group_id = aws_security_group.producer.id
    }
  }
  endpoints = {
    s3 = {
      service         = "s3"
      service_type    = "Gateway"
      route_table_ids = concat(module.producer_vpc.intra_route_table_ids, module.producer_vpc.public_route_table_ids)
    }
    glue = {
      service             = "glue"
      private_dns_enabled = true
    }
  }
}

module "consumer_endpoints" {
  source    = "terraform-aws-modules/vpc/aws//modules/vpc-endpoints"
  version   = "6.6.1"
  providers = { aws = aws.consumer }

  vpc_id                     = module.consumer_vpc.vpc_id
  subnet_ids                 = local.consumer_subnet_ids
  create_security_group      = true
  security_group_name_prefix = "${local.name}-consumer-endpoints-"
  security_group_description = "HTTPS from consumer warehouse to private endpoints"
  security_group_rules = {
    warehouse_https = {
      description              = "Consumer warehouse HTTPS"
      source_security_group_id = aws_security_group.consumer.id
    }
  }
  endpoints = merge({
    s3 = {
      service         = "s3"
      service_type    = "Gateway"
      route_table_ids = concat(module.consumer_vpc.intra_route_table_ids, module.consumer_vpc.public_route_table_ids)
    }
    glue = {
      service             = "glue"
      private_dns_enabled = true
    }
    }, local.sso_enabled ? {
    sso_oauth = {
      service             = "sso-oauth"
      private_dns_enabled = true
    }
    identitystore = {
      service             = "identitystore"
      private_dns_enabled = true
    }
  } : {})
}
