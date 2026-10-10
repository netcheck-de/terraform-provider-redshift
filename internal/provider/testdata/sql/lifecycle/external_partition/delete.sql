-- database: admin
SELECT columnname, external_type, columnnum, part_key FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"admin","schema":"example_external","table":"events"}

-- database: admin
SELECT values, location FROM svv_external_partitions WHERE LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table);
-- params: {"schema":"example_external","table":"events"}

-- database: admin
ALTER TABLE "example_external"."events" DROP PARTITION ("event_date" = '2024-01-01');
-- params: {}

-- database: admin
SELECT columnname, external_type, columnnum, part_key FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"admin","schema":"example_external","table":"events"}

-- database: admin
SELECT values, location FROM svv_external_partitions WHERE LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table);
-- params: {"schema":"example_external","table":"events"}
