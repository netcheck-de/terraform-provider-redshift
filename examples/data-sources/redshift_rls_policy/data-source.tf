data "redshift_rls_policy" "own_region" {
  database = "analytics"
  name     = "own_region"
}
