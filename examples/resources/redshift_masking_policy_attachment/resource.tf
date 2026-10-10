resource "redshift_masking_policy_attachment" "email_analysts" {
  database     = redshift_masking_policy.email.database
  policy       = redshift_masking_policy.email.name
  schema       = "public"
  relation     = "customers"
  columns      = ["email"]
  grantee      = redshift_role.analysts.name
  grantee_type = "ROLE"
  priority     = 10
}

# The card policy masks card_number and also reads is_fraud, so the input mapping lists both relation columns.
resource "redshift_masking_policy_attachment" "card_public" {
  database      = redshift_masking_policy.card.database
  policy        = redshift_masking_policy.card.name
  schema        = "public"
  relation      = "payments"
  columns       = ["card_number"]
  input_columns = ["is_fraud", "card_number"]
  grantee       = "public"
  grantee_type  = "PUBLIC"
}
