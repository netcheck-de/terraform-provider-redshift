terraform {
  required_version = ">= 1.14"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.67"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.7"
    }
    redshift = {
      source  = "netcheck-de/redshift"
      version = "0.2.0"
    }
  }
}

# The secret module declares an ephemeral password type even when generation is disabled.
# Keep its Random provider separate so composition tests can mock the example's passwords.
provider "random" {
  alias = "secrets"
}

provider "aws" {
  alias   = "producer"
  region  = var.region
  profile = local.producer_profile
  default_tags { tags = { Project = "terraform-provider-redshift-example" } }
}

provider "aws" {
  alias   = "consumer"
  region  = var.region
  profile = local.consumer_profile
  default_tags { tags = { Project = "terraform-provider-redshift-example" } }
}

# Managed administrator secrets make a fresh apply independent of the caller's SQL identity.
provider "redshift" {
  alias              = "producer"
  region             = var.region
  profile            = local.producer_profile
  database           = aws_redshift_cluster.producer.database_name
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
}

provider "redshift" {
  alias          = "consumer"
  region         = var.region
  profile        = local.consumer_profile
  database       = aws_redshiftserverless_namespace.consumer.db_name
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
  secret_arn     = aws_redshiftserverless_namespace.consumer.admin_password_secret_arn
}

# Optional read-only probes cover IAM and direct password connections.
provider "redshift" {
  alias              = "producer_data_api_iam"
  region             = var.region
  profile            = local.producer_profile
  database           = aws_redshift_cluster.producer.database_name
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  db_user            = aws_redshift_cluster.producer.master_username
}

provider "redshift" {
  alias          = "consumer_data_api_iam"
  region         = var.region
  profile        = local.consumer_profile
  database       = aws_redshiftserverless_namespace.consumer.db_name
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
}

provider "redshift" {
  alias    = "producer_direct_iam"
  region   = var.region
  profile  = local.producer_profile
  database = aws_redshift_cluster.producer.database_name
  direct_connection {
    iam {
      cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
      db_user            = aws_redshift_cluster.producer.master_username
    }
  }
}

provider "redshift" {
  alias    = "consumer_direct_iam"
  region   = var.region
  profile  = local.consumer_profile
  database = aws_redshiftserverless_namespace.consumer.db_name
  direct_connection {
    iam {
      workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
    }
  }
}

provider "redshift" {
  alias    = "producer_direct_password"
  database = aws_redshift_cluster.producer.database_name
  direct_connection {
    host     = aws_redshift_cluster.producer.dns_name
    port     = aws_redshift_cluster.producer.port
    username = aws_redshift_cluster.producer.master_username
    password = var.producer_password
  }
}

provider "redshift" {
  alias    = "consumer_direct_password"
  database = aws_redshiftserverless_namespace.consumer.db_name
  direct_connection {
    host     = aws_redshiftserverless_workgroup.consumer.endpoint[0].address
    port     = aws_redshiftserverless_workgroup.consumer.endpoint[0].port
    username = aws_redshiftserverless_namespace.consumer.admin_username
    password = var.consumer_password
  }
}

data "aws_caller_identity" "producer" { provider = aws.producer }
data "aws_caller_identity" "consumer" { provider = aws.consumer }
data "aws_partition" "producer" { provider = aws.producer }
data "aws_partition" "consumer" { provider = aws.consumer }

resource "random_id" "environment" { byte_length = 4 }

locals {
  name             = "${var.name_prefix}-${random_id.environment.hex}"
  producer_profile = var.producer_profile != null ? var.producer_profile : var.profile
  consumer_profile = var.consumer_profile != null ? var.consumer_profile : var.profile
  cross_account    = data.aws_caller_identity.producer.account_id != data.aws_caller_identity.consumer.account_id
  sso_enabled      = var.identity_center_instance_arn != null
}
