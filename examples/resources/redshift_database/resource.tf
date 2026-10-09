resource "redshift_database" "analytics" {
  name          = "analytics"
  datashare_arn = aws_redshift_data_share_consumer_association.analytics.data_share_arn
}

resource "redshift_database" "local" {
  name = "warehouse"
}
