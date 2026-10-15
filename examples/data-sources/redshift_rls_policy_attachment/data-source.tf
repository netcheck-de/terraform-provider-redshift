data "redshift_rls_policy_attachment" "analysts" {
  policy       = "own_region"
  database     = "analytics"
  schema       = "sales"
  relation     = "orders"
  grantee      = "analysts"
  grantee_type = "ROLE"
}
