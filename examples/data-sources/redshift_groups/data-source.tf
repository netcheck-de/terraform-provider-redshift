data "redshift_groups" "all" {}

output "group_members" {
  value = { for group in data.redshift_groups.all.groups : group.name => group.members }
}
