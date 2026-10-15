-- database: warehouse
CREATE TEMPLATE "serving"."csv_load" FOR COPY AS FORMAT AS CSV DELIMITER ',' IGNOREHEADER 1;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: warehouse
SHOW TEMPLATES FROM SCHEMA "serving" LIKE 'csv_load';
-- params: {}

-- database: warehouse
SHOW TEMPLATE "serving"."csv_load";
-- params: {}
