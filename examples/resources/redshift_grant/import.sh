# Role grant
terraform import 'redshift_grant.readers["TABLES"]' \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","role":"ncidc:analytics-readers","scope":"TABLES"}'

# User grant: uses user in place of role and includes schema_name when set
terraform import redshift_grant.analyst_tables \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","user":"analyst","schema_name":"serving","scope":"TABLES"}'

# Datashare grant: uses datashare in place of role and includes schema_name
terraform import redshift_grant.share_tables \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","datashare":"analytics_share","schema_name":"serving","scope":"TABLES"}'
