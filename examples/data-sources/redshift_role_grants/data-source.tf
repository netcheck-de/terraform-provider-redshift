data "redshift_role_grants" "readers" {
  role = "report_readers"
}

output "report_reader_users" {
  value = [for grant in data.redshift_role_grants.readers.role_grants : grant.to_user if grant.to_user != null]
}
