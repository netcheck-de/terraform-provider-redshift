-- database: admin
SELECT schemaname, tablename, location, input_format, output_format, serialization_lib, serde_parameters, parameters FROM svv_external_tables WHERE redshift_database_name = :database ORDER BY schemaname, tablename;
-- params: {"database":"admin"}

-- database: admin
SELECT columnname, external_type, columnnum, part_key, schemaname, tablename FROM svv_external_columns WHERE redshift_database_name = :database ORDER BY schemaname, tablename, columnnum;
-- params: {"database":"admin"}
