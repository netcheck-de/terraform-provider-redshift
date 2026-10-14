-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"admin","schema":"serving"}

-- database: admin
SHOW TEMPLATES FROM SCHEMA "serving" LIKE 'csv_load';
-- params: {}

-- database: admin
SHOW TEMPLATE "serving"."csv_load";
-- params: {}

-- database: admin
ALTER TEMPLATE "serving"."csv_load" DROP DELIMITER;
-- params: {}

-- database: admin
ALTER TEMPLATE "serving"."csv_load" SET IGNOREHEADER TO 2;
-- params: {}

-- database: admin
ALTER TEMPLATE "serving"."csv_load" ADD GZIP;
-- params: {}

-- database: admin
ALTER TEMPLATE "serving"."csv_load" ADD NULL AS '';
-- params: {}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"admin","schema":"serving"}

-- database: admin
SHOW TEMPLATES FROM SCHEMA "serving" LIKE 'csv_load';
-- params: {}

-- database: admin
SHOW TEMPLATE "serving"."csv_load";
-- params: {}
