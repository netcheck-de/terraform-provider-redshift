# The masking expression reads a lookup table, which the policy itself needs SELECT on.
resource "redshift_policy_grant" "exempt_lookup" {
  database_name = redshift_masking_policy.email.database
  schema_name   = "public"
  object_name   = "masking_exempt_emails"
  policy_type   = "MASKING"
  policy_name   = redshift_masking_policy.email.name
  privileges    = ["SELECT"]
}
