terraform import redshift_debug_grant.auditor \
  '{"database":"sales_db@finance-catalog","grantee":"IAM:sales_auditor","grantee_type":"USER"}'
