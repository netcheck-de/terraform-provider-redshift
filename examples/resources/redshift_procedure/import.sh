terraform import redshift_procedure.purge \
  '{"workgroup_name":"warehouse","database":"analytics","schema":"reporting","name":"sp_purge_events","arguments":"integer"}'
