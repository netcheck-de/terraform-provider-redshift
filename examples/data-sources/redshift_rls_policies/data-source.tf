data "redshift_rls_policies" "analytics" {
  database = "analytics"
}

output "rls_policy_names" {
  value = data.redshift_rls_policies.analytics.rls_policies[*].name
}
