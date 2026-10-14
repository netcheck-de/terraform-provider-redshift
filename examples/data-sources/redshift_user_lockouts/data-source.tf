data "redshift_user_lockouts" "all" {}

data "redshift_user_lockouts" "reader" {
  user_name = "report_reader"
}

output "locked_users" {
  value = [for user in data.redshift_user_lockouts.all.user_lockouts : user.user_name if user.is_locked]
}
