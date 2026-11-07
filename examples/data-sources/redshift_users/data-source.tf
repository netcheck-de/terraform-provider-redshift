data "redshift_users" "all" {}

output "superusers" {
  value = [for user in data.redshift_users.all.users : user.name if user.superuser]
}
