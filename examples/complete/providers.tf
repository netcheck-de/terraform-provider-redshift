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

# The workgroup selector also accepts the workgroup ARN.
provider "redshift" {
  alias          = "consumer_data_api_iam"
  region         = var.region
  profile        = local.consumer_profile
  database       = aws_redshiftserverless_namespace.consumer.db_name
  workgroup_name = aws_redshiftserverless_workgroup.consumer.arn
}

provider "redshift" {
  alias    = "producer_direct_iam"
  region   = var.region
  profile  = local.producer_profile
  database = aws_redshift_cluster.producer.database_name
  direct_connection {
    sslmode = var.direct_sslmode
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
  # IAM mode discovers the endpoint; host and port override the discovered values.
  direct_connection {
    sslmode = var.direct_sslmode
    port    = aws_redshiftserverless_workgroup.consumer.endpoint[0].port
    iam {
      workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name
    }
  }
}

# Terraform validates every provider block, even when its probe is disabled. A disabled probe gets a placeholder
# password; no SQL runs through it because its lookup has count = 0.
provider "redshift" {
  alias    = "producer_direct_password"
  database = aws_redshift_cluster.producer.database_name
  direct_connection {
    sslmode  = var.direct_sslmode
    host     = aws_redshift_cluster.producer.dns_name
    port     = aws_redshift_cluster.producer.port
    username = aws_redshift_cluster.producer.master_username
    password = coalesce(var.producer_password, "disabled-probe")
  }
}

provider "redshift" {
  alias    = "consumer_direct_password"
  database = aws_redshiftserverless_namespace.consumer.db_name
  direct_connection {
    sslmode  = var.direct_sslmode
    host     = aws_redshiftserverless_workgroup.consumer.endpoint[0].address
    port     = aws_redshiftserverless_workgroup.consumer.endpoint[0].port
    username = aws_redshiftserverless_namespace.consumer.admin_username
    password = coalesce(var.consumer_password, "disabled-probe")
  }
}
