data "redshift_masking_policies" "warehouse" {
  database = "warehouse"
}

output "masking_policy_names" {
  value = data.redshift_masking_policies.warehouse.masking_policies[*].name
}
