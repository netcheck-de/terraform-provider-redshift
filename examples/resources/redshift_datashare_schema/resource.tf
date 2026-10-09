resource "redshift_datashare_schema" "serving" {
  database    = redshift_datashare.producer.database
  datashare   = redshift_datashare.producer.name
  schema      = redshift_schema.serving.name
  include_new = true
}
