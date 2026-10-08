# Optional read-only probes retain every connection method in the unified example.
# Provider configurations are in main.tf.

data "redshift_database" "producer_data_api_iam" {
  provider = redshift.producer_data_api_iam
  count    = contains(var.connection_checks, "producer_data_api_iam") ? 1 : 0
  name     = aws_redshift_cluster.producer.database_name
}

data "redshift_database" "consumer_data_api_iam" {
  provider = redshift.consumer_data_api_iam
  count    = contains(var.connection_checks, "consumer_data_api_iam") ? 1 : 0
  name     = aws_redshiftserverless_namespace.consumer.db_name
}

data "redshift_database" "producer_direct_iam" {
  provider = redshift.producer_direct_iam
  count    = contains(var.connection_checks, "producer_direct_iam") ? 1 : 0
  name     = aws_redshift_cluster.producer.database_name
}

data "redshift_database" "consumer_direct_iam" {
  provider = redshift.consumer_direct_iam
  count    = contains(var.connection_checks, "consumer_direct_iam") ? 1 : 0
  name     = aws_redshiftserverless_namespace.consumer.db_name
}

data "redshift_database" "producer_direct_password" {
  provider = redshift.producer_direct_password
  count    = contains(var.connection_checks, "producer_direct_password") ? 1 : 0
  name     = aws_redshift_cluster.producer.database_name
}

data "redshift_database" "consumer_direct_password" {
  provider = redshift.consumer_direct_password
  count    = contains(var.connection_checks, "consumer_direct_password") ? 1 : 0
  name     = aws_redshiftserverless_namespace.consumer.db_name
}
