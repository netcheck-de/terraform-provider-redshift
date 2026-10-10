resource "redshift_rls_policy_attachment" "analysts" {
  policy       = redshift_rls_policy.own_region.name
  database     = redshift_rls_policy.own_region.database
  schema       = "sales"
  relation     = "orders"
  grantee      = "analysts"
  grantee_type = "ROLE"

  # DROP RLS POLICY refuses an attached policy, so a change that replaces the policy must detach it first.
  lifecycle {
    replace_triggered_by = [redshift_rls_policy.own_region.column, redshift_rls_policy.own_region.alias]
  }
}

resource "redshift_rls_policy_attachment" "everyone" {
  policy       = redshift_rls_policy.own_region.name
  database     = redshift_rls_policy.own_region.database
  schema       = "sales"
  relation     = "orders"
  grantee      = "public"
  grantee_type = "PUBLIC"

  lifecycle {
    replace_triggered_by = [redshift_rls_policy.own_region.column, redshift_rls_policy.own_region.alias]
  }
}
