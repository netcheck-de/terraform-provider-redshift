terraform import redshift_assumerole_grant.loader \
  '{"workgroup_name":"warehouse","database":"admin","iam_role_arn":"default","grantee":"loader","grantee_type":"ROLE"}'
