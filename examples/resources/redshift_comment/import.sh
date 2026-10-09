terraform import redshift_comment.column \
  '{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","object_type":"COLUMN","schema_name":"reporting","object_name":"daily_summary","column_name":"day"}'
