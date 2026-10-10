-- database: warehouse
CREATE EXTERNAL TABLE "example_external"."events" ("id" integer, "label" varchar(64)) PARTITIONED BY ("event_date" date) ROW FORMAT DELIMITED FIELDS TERMINATED BY ',' STORED AS TEXTFILE LOCATION 's3://example-bucket/events/' TABLE PROPERTIES ('skip.header.line.count' = '1');
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT schemaname, tablename, location, input_format, output_format, serialization_lib, serde_parameters, parameters FROM svv_external_tables WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:name);
-- params: {"database":"warehouse","name":"events","schema":"example_external"}

-- database: warehouse
SELECT columnname, external_type, columnnum, part_key FROM svv_external_columns WHERE redshift_database_name = :database AND LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table) ORDER BY columnnum;
-- params: {"database":"warehouse","schema":"example_external","table":"events"}
