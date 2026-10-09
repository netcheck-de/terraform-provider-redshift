# Role grant
terraform import 'redshift_grant.readers["TABLES"]' \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","role":"ncidc:analytics-readers","scope":"TABLES"}'

# Datashare grant: uses datashare in place of role and includes schema_name
terraform import redshift_grant.share_tables \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","datashare":"analytics_share","schema_name":"serving","scope":"TABLES"}'
