# Fixture SQL uses the pinned Registry release, never the development provider under test.
provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project = "terraform-provider-redshift-acceptance"
      TestRun = var.name_prefix
    }
  }
}

provider "redshift" {
  region         = var.region
  database       = var.db_name
  workgroup_name = aws_redshiftserverless_workgroup.this.workgroup_name
  secret_arn     = aws_redshiftserverless_namespace.this.admin_password_secret_arn
}

provider "redshift" {
  alias              = "producer"
  region             = var.region
  database           = var.db_name
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
}
