terraform import redshift_object_grant.report \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","schema_name":"reporting","object_name":"daily_summary","object_type":"TABLE","grantee":"report_readers","grantee_type":"GROUP"}'

# Function overload: arguments as configured
terraform import redshift_object_grant.score \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","schema_name":"reporting","object_name":"f_score","object_type":"FUNCTION","arguments":"integer, varchar","grantee":"analyst","grantee_type":"USER"}'
