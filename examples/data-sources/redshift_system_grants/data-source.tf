data "redshift_system_grants" "all" {}

output "system_grants" {
  value = { for grant in data.redshift_system_grants.all.system_grants : grant.role => grant.privileges }
}
