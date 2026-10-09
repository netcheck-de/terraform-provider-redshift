terraform import redshift_object_grant.report \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","schema_name":"reporting","object_name":"daily_summary","object_type":"TABLE","grantee":"report_readers","grantee_type":"GROUP"}'
