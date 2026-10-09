resource "redshift_datashare_grant" "consumer" {
  database   = redshift_datashare.producer.database
  datashare  = redshift_datashare.producer.name
  account_id = var.consumer_account_id
}

resource "aws_redshift_data_share_authorization" "consumer" {
  provider            = aws.producer
  data_share_arn      = local.datashare_arn
  consumer_identifier = var.consumer_account_id

  depends_on = [redshift_datashare_grant.consumer]
}

resource "aws_redshift_data_share_consumer_association" "consumer" {
  provider       = aws.consumer
  data_share_arn = aws_redshift_data_share_authorization.consumer.data_share_arn
  consumer_arn   = var.consumer_namespace_arn
}
