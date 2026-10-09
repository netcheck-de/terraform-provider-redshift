terraform {
  required_providers {
    redshift = {
      source  = "netcheck-de/redshift"
      version = "~> 0.2"
    }
  }
}

# Serverless through the Data API with the caller's IAM identity.
provider "redshift" {
  region         = "eu-central-1"
  profile        = "warehouse-admin"
  workgroup_name = "analytics"
  database       = "dev"
}

# Serverless through the Data API with Secrets Manager credentials.
provider "redshift" {
  alias          = "serverless_secret"
  region         = "eu-central-1"
  workgroup_name = "analytics"
  database       = "dev"
  secret_arn     = "arn:aws:secretsmanager:eu-central-1:123456789012:secret:redshift-admin-AbCdEf"
}

# Provisioned cluster through the Data API with temporary credentials for an existing SQL user.
provider "redshift" {
  alias              = "cluster_data_api"
  region             = "eu-central-1"
  cluster_identifier = "analytics-cluster"
  db_user            = "terraform_admin"
  database           = "dev"
}

# Direct TLS connection with IAM credentials; the endpoint is discovered from the workgroup.
provider "redshift" {
  alias    = "serverless_direct_iam"
  region   = "eu-central-1"
  database = "dev"
  direct_connection {
    iam {
      workgroup_name = "analytics"
    }
  }
}

# Direct TLS connection to a cluster through a private endpoint, trusting an additional CA bundle.
provider "redshift" {
  alias    = "cluster_direct_iam"
  region   = "eu-central-1"
  database = "dev"
  direct_connection {
    host         = "redshift.internal.example.com"
    ca_cert_file = "/etc/ssl/certs/internal-ca.pem"
    iam {
      cluster_identifier = "analytics-cluster"
      db_user            = "terraform_admin"
    }
  }
}

# Direct password connection; no AWS credentials are needed.
variable "redshift_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

provider "redshift" {
  alias    = "direct_password"
  database = "dev"
  direct_connection {
    host     = "analytics.123456789012.eu-central-1.redshift-serverless.amazonaws.com"
    username = "terraform_admin"
    password = var.redshift_password
    # verify-full is the default; weaker modes are an explicit opt-in.
    sslmode = "verify-full"
  }
}
