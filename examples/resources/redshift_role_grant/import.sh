terraform import redshift_role_grant.operators \
  '{"workgroup_name":"warehouse","database":"admin","role":"sys:dba","to_role":"ncidc:analytics-operators"}'
