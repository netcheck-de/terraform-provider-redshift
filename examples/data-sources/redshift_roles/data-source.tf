data "redshift_roles" "all" {}

output "custom_roles" {
  value = [for role in data.redshift_roles.all.roles : role.name if !startswith(role.name, "sys:")]
}
