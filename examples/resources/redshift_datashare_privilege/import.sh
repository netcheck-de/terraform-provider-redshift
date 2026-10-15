terraform import redshift_datashare_privilege.share_admins \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"warehouse","datashare_name":"analytics","grantee":"share_admins","grantee_type":"ROLE"}'
