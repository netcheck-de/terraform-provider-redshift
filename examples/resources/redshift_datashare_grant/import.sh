# Account grant
terraform import redshift_datashare_grant.consumer \
  '{"workgroup_name":"warehouse","database":"warehouse","datashare":"analytics","account_id":"123456789012"}'

# Namespace grant: uses namespace_id instead of account_id
terraform import redshift_datashare_grant.namespace \
  '{"workgroup_name":"warehouse","database":"warehouse","datashare":"analytics","namespace_id":"12345678-1234-1234-1234-123456789abc"}'

# Lake Formation account grant: adds via_data_catalog
terraform import redshift_datashare_grant.lake_formation \
  '{"workgroup_name":"warehouse","database":"warehouse","datashare":"analytics","account_id":"123456789012","via_data_catalog":"true"}'
