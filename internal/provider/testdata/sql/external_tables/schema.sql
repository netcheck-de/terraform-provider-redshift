-- database: admin
SELECT schemaname, tablename, location, input_format, output_format, serialization_lib, serde_parameters, parameters FROM svv_external_tables WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) ORDER BY schemaname, tablename;
-- params: {"database":"admin","schema":"EXAMPLE_EXTERNAL"}

-- database: admin
SELECT columnname, external_type, columnnum, part_key, schemaname, tablename FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) ORDER BY schemaname, tablename, columnnum;
-- params: {"database":"admin","schema":"EXAMPLE_EXTERNAL"}
