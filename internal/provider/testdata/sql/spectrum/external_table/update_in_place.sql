-- database: admin
SELECT schemaname, tablename, location, input_format, output_format, serialization_lib, serde_parameters, parameters FROM svv_external_tables WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:name);
-- params: {"database":"admin","name":"events","schema":"example_external"}

-- database: admin
SELECT columnname, external_type, columnnum, part_key FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"admin","schema":"example_external","table":"events"}

-- database: admin
ALTER TABLE "example_external"."events" ADD COLUMN "amount" decimal(8, 2);
-- params: {}

-- database: admin
ALTER TABLE "example_external"."events" DROP COLUMN "id";
-- params: {}

-- database: admin
ALTER TABLE "example_external"."events" SET FILE FORMAT SEQUENCEFILE;
-- params: {}

-- database: admin
ALTER TABLE "example_external"."events" SET LOCATION 's3://example-bucket/events-v2/';
-- params: {}

-- database: admin
ALTER TABLE "example_external"."events" SET TABLE PROPERTIES ('skip.header.line.count' = '2');
-- params: {}

-- database: admin
SELECT schemaname, tablename, location, input_format, output_format, serialization_lib, serde_parameters, parameters FROM svv_external_tables WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:name);
-- params: {"database":"admin","name":"events","schema":"example_external"}

-- database: admin
SELECT columnname, external_type, columnnum, part_key FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"admin","schema":"example_external","table":"events"}
