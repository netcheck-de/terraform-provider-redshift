# Data API IAM probes run by default; direct probes require explicit connectivity and credential inputs.
# Provider configurations are in providers.tf.

data "redshift_database" "producer_data_api_iam" {
  provider = redshift.producer_data_api_iam
  count    = contains(var.connection_checks, "producer_data_api_iam") ? 1 : 0
  name     = aws_redshift_cluster.producer.database_name

  lifecycle {
    postcondition {
      condition     = try(upper(self.database_type) == "LOCAL", false)
      error_message = "The producer Data API IAM connection must read an ordinary local administration database."
    }
  }
}

data "redshift_database" "consumer_data_api_iam" {
  provider = redshift.consumer_data_api_iam
  count    = contains(var.connection_checks, "consumer_data_api_iam") ? 1 : 0
  name     = aws_redshiftserverless_namespace.consumer.db_name

  lifecycle {
    postcondition {
      condition     = try(upper(self.database_type) == "LOCAL", false)
      error_message = "The consumer Data API IAM connection must read an ordinary local administration database."
    }
  }
}

data "redshift_database" "producer_direct_iam" {
  provider = redshift.producer_direct_iam
  count    = contains(var.connection_checks, "producer_direct_iam") ? 1 : 0
  name     = aws_redshift_cluster.producer.database_name

  lifecycle {
    postcondition {
      condition     = try(upper(self.database_type) == "LOCAL", false)
      error_message = "The producer direct IAM connection must read an ordinary local administration database."
    }
  }
}

data "redshift_database" "consumer_direct_iam" {
  provider = redshift.consumer_direct_iam
  count    = contains(var.connection_checks, "consumer_direct_iam") ? 1 : 0
  name     = aws_redshiftserverless_namespace.consumer.db_name

  lifecycle {
    postcondition {
      condition     = try(upper(self.database_type) == "LOCAL", false)
      error_message = "The consumer direct IAM connection must read an ordinary local administration database."
    }
  }
}

data "redshift_database" "producer_direct_password" {
  provider = redshift.producer_direct_password
  count    = contains(var.connection_checks, "producer_direct_password") ? 1 : 0
  name     = aws_redshift_cluster.producer.database_name

  lifecycle {
    postcondition {
      condition     = try(upper(self.database_type) == "LOCAL", false)
      error_message = "The producer direct password connection must read an ordinary local administration database."
    }
  }
}

data "redshift_database" "consumer_direct_password" {
  provider = redshift.consumer_direct_password
  count    = contains(var.connection_checks, "consumer_direct_password") ? 1 : 0
  name     = aws_redshiftserverless_namespace.consumer.db_name

  lifecycle {
    postcondition {
      condition     = try(upper(self.database_type) == "LOCAL", false)
      error_message = "The consumer direct password connection must read an ordinary local administration database."
    }
  }
}
