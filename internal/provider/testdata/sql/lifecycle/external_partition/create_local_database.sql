-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT columnname, external_type, columnnum, part_key FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"warehouse","schema":"example_external","table":"events"}

-- database: warehouse
ALTER TABLE "example_external"."events" ADD PARTITION ("event_date" = '2024-01-01') LOCATION 's3://example-bucket/events/event_date=2024-01-01/';
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT columnname, external_type, columnnum, part_key FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"warehouse","schema":"example_external","table":"events"}

-- database: warehouse
SELECT values, location FROM svv_external_partitions WHERE LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table);
-- params: {"schema":"example_external","table":"events"}
