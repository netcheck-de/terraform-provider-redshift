terraform import redshift_column_grant.events \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","schema_name":"reporting","object_name":"events","grantee":"analysts","grantee_type":"ROLE"}'
