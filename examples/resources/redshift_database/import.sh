terraform import redshift_database.analytics \
  '{"workgroup_name":"warehouse","database":"admin","name":"analytics","datashare_arn":"arn:aws:redshift:eu-central-1:123456789012:datashare:11111111-2222-3333-4444-555555555555/source"}'
