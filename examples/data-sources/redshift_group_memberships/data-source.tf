data "redshift_group_memberships" "readers" {
  group = "report_readers"
}

output "report_readers" {
  value = [for membership in data.redshift_group_memberships.readers.group_memberships : membership.user]
}
