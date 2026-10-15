terraform import redshift_rls_policy_attachment.analysts \
  '{"workgroup_name":"warehouse","database":"analytics","policy":"own_region","schema":"sales","relation":"orders","grantee":"analysts","grantee_type":"ROLE"}'
