output "masking_checks" {
  description = "Read-only observations of the masking policy, its attachment, and the policy listing."
  value = {
    policy_expression = data.redshift_masking_policy.email.expression
    policy_inputs     = [for column in data.redshift_masking_policy.email.input_columns : "${column.name} ${column.type}"]
    attached          = data.redshift_masking_policy_attachment.email_readers.exists
    attached_priority = data.redshift_masking_policy_attachment.email_readers.priority
    database_policies = [for policy in data.redshift_masking_policies.local.masking_policies : policy.name]
  }
}
