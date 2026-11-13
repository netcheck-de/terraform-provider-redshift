# Every command each identity may run with the namespace default IAM role.
data "redshift_assumerole_grants" "default_role" {
  iam_role_arn = "DEFAULT"
}

output "default_role_commands" {
  value = { for grant in data.redshift_assumerole_grants.default_role.assumerole_grants : "${grant.grantee_type} ${grant.grantee}" => grant.privileges }
}
