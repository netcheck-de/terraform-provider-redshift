data "aws_availability_zones" "available" {
  state = "available"
  filter {
    name   = "opt-in-status"
    values = ["opt-in-not-required"]
  }
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "6.7.3"

  name = var.name_prefix
  cidr = var.vpc_cidr
  azs  = slice(data.aws_availability_zones.available.names, 0, 3)

  public_subnets                = [for i in range(3) : cidrsubnet(var.vpc_cidr, 8, i)]
  redshift_subnets              = [for i in range(3) : cidrsubnet(var.vpc_cidr, 8, i + 3)]
  enable_public_redshift        = true
  create_redshift_subnet_group  = true
  manage_default_network_acl    = false
  manage_default_route_table    = false
  manage_default_security_group = false
}

resource "aws_security_group" "this" {
  name        = var.name_prefix
  description = "Disposable acceptance fixture; SQL ingress only from the current runner"
  vpc_id      = module.vpc.vpc_id
}

resource "aws_vpc_security_group_ingress_rule" "this" {
  security_group_id = aws_security_group.this.id
  cidr_ipv4         = var.runner_cidr
  ip_protocol       = "tcp"
  from_port         = 5439
  to_port           = 5439
}

resource "aws_vpc_security_group_egress_rule" "this" {
  security_group_id = aws_security_group.this.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
