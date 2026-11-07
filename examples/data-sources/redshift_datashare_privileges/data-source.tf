data "redshift_datashare_privileges" "reporting" {
  datashare_name = "reporting"
}

output "share_administrators" {
  value = { for item in data.redshift_datashare_privileges.reporting.datashare_privileges : "${item.grantee_type}:${item.grantee}" => item.privileges }
}
