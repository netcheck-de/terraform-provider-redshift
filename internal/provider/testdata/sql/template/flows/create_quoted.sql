-- database: admin
CREATE TEMPLATE "serving"."it's" FOR COPY AS DELIMITER '''' NULL AS '\\N';
-- params: {}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"admin","schema":"serving"}

-- database: admin
SHOW TEMPLATES FROM SCHEMA "serving" LIKE 'it''s';
-- params: {}

-- database: admin
SHOW TEMPLATE "serving"."it's";
-- params: {}
