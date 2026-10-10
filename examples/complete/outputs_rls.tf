output "row_level_security" {
  description = "Read-only observations of the row-level security policy, its attachment, and the protected table."
  value = {
    policy = {
      id          = data.redshift_rls_policy.own_region.id
      predicate   = data.redshift_rls_policy.own_region.predicate
      column      = data.redshift_rls_policy.own_region.column
      fingerprint = data.redshift_rls_policy.own_region.definition_fingerprint
    }
    attached = data.redshift_rls_policy_attachment.readers.exists
    table = {
      row_level_security           = data.redshift_table_security.rls_events.row_level_security
      conjunction_type             = data.redshift_table_security.rls_events.conjunction_type
      datashare_row_level_security = data.redshift_table_security.rls_events.datashare_row_level_security
    }
    policies = [for policy in data.redshift_rls_policies.local.rls_policies : policy.name]
  }
}
