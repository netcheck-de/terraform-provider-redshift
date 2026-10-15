resource "redshift_masking_policy" "email" {
  database   = redshift_database.warehouse.name
  name       = "mask_email"
  expression = "REGEXP_REPLACE(email, '^[^@]+', '***')"

  input_column {
    name = "email"
    type = "VARCHAR(256)"
  }
}

# A conditional policy reads a second column, which the attachment maps through input_columns.
resource "redshift_masking_policy" "card" {
  database   = redshift_database.warehouse.name
  name       = "mask_card"
  expression = "CASE WHEN is_fraud THEN pan ELSE NULL END"

  input_column {
    name = "is_fraud"
    type = "BOOLEAN"
  }

  input_column {
    name = "pan"
    type = "VARCHAR(16)"
  }
}
