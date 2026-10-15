resource "redshift_datashare_grant" "lake_formation" {
  database         = redshift_datashare.producer.database
  datashare        = redshift_datashare.producer.name
  account_id       = var.lake_formation_account_id
  via_data_catalog = true
}

resource "aws_redshift_data_share_authorization" "lake_formation" {
  provider            = aws.producer
  data_share_arn      = local.datashare_arn
  consumer_identifier = "DataCatalog/${var.lake_formation_account_id}"

  depends_on = [redshift_datashare_grant.lake_formation]
}
