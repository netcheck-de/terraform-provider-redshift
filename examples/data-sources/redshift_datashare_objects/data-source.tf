data "redshift_datashare_objects" "example" {
  database  = "analytics"
  datashare = "reporting"

  # Optional: omit to observe schemas, relations, and routines together.
  object_type = "function"
}
