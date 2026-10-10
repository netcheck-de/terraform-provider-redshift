resource "redshift_masking_policy" "email" {
  database = redshift_database.warehouse.name
  name     = "mask_email"

  input_columns = [
    { name = "email", type = "VARCHAR(256)" },
  ]
  expression = "REGEXP_REPLACE(email, '^[^@]+', '***')"
}

# A conditional policy reads a second column, which the attachment maps through input_columns.
resource "redshift_masking_policy" "card" {
  database = redshift_database.warehouse.name
  name     = "mask_card"

  input_columns = [
    { name = "is_fraud", type = "BOOLEAN" },
    { name = "pan", type = "VARCHAR(16)" },
  ]
  expression = "CASE WHEN is_fraud THEN pan ELSE NULL END"
}
