terraform import redshift_external_partition.sales_2008_01 \
  '{"workgroup_name":"warehouse","database":"warehouse","schema":"raw","table":"sales","values":"{\"sale_date\":\"2008-01-01\"}"}'
