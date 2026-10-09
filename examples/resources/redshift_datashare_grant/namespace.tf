resource "redshift_datashare_grant" "namespace" {
  database     = redshift_datashare.producer.database
  datashare    = redshift_datashare.producer.name
  namespace_id = var.consumer_namespace_id
}
