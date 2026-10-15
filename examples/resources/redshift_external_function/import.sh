terraform import redshift_external_function.upper \
  '{"workgroup_name":"warehouse","database":"warehouse","schema":"public","name":"f_upper","arguments":"character varying"}'
