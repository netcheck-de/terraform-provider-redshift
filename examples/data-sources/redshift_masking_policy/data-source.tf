data "redshift_masking_policy" "email" {
  database = "warehouse"
  name     = "mask_email"
}
