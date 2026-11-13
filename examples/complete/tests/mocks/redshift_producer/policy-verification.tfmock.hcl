# The administrator database lookup is independent of the managed producer database.
# Database names are selector inputs; Terraform's mock retains them as supplied by the AWS fixtures.
override_data {
  target = data.redshift_database.producer_default_connection
  values = { owner = "admin" }
}
