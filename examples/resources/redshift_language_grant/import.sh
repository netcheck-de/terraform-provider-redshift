terraform import redshift_language_grant.procedures \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","language_name":"plpgsql","grantee":"developers","grantee_type":"ROLE"}'
