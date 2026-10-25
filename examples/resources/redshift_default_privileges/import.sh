terraform import redshift_default_privileges.reports \
  '{"database_name":"analytics","owner":"loader","schema_name":"reporting","object_type":"TABLES","grantee":"report_readers","grantee_type":"GROUP"}'

# Defaults of the connection user: no owner key
terraform import redshift_default_privileges.mine \
  '{"database_name":"analytics","object_type":"TABLES","grantee":"analyst","grantee_type":"USER"}'
