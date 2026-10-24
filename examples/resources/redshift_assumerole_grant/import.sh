terraform import redshift_assumerole_grant.loader \
  '{"workgroup_name":"warehouse","database":"admin","iam_role_arn":"DEFAULT","grantee":"loader","grantee_type":"ROLE"}'
