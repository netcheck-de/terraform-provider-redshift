data "redshift_masking_policy_attachment" "email_analysts" {
  database     = "warehouse"
  policy       = "mask_email"
  schema       = "public"
  relation     = "customers"
  columns      = ["email"]
  grantee      = "analysts"
  grantee_type = "ROLE"
}
