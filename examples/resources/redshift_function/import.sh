terraform import redshift_function.greater \
  '{"workgroup_name":"warehouse","database":"analytics","schema":"reporting","name":"f_sql_greater","arguments":"DOUBLE PRECISION, DOUBLE PRECISION"}'
