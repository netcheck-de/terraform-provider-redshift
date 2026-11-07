-- database: admin
SELECT columnname, external_type, columnnum, part_key, schemaname, tablename FROM svv_external_columns WHERE redshift_database_name = :database ORDER BY schemaname, tablename, columnnum;
-- params: {"database":"admin"}

-- database: admin
SELECT values, location, schemaname, tablename FROM svv_external_partitions;
-- params: {}
