# Attach the policies first, so readers are never left without a policy once RLS is on.
resource "redshift_table_security" "orders" {
  database           = "analytics"
  schema             = "sales"
  relation           = "orders"
  row_level_security = true
  conjunction_type   = "AND"

  depends_on = [redshift_rls_policy_attachment.analysts]
}
