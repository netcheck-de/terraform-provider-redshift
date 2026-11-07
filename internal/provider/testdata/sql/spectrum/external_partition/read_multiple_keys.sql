-- database: admin
SELECT columnname, external_type, columnnum, part_key, schemaname, tablename FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"admin","schema":"example_external","table":"events"}

-- database: admin
SELECT values, location, schemaname, tablename FROM svv_external_partitions WHERE LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table);
-- params: {"schema":"example_external","table":"events"}
