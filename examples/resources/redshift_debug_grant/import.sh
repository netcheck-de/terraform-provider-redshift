terraform import redshift_debug_grant.auditor \
  '{"workgroup_name":"warehouse","database":"sales_db@finance-catalog","grantee":"IAM:sales_auditor","grantee_type":"USER"}'
