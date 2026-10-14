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
CREATE OR REPLACE TEMPLATE "serving"."csv_load" FOR COPY AS FORMAT AS JSON 'auto ignorecase' GZIP;
-- params: {}

-- database: admin
ALTER TEMPLATE "serving"."csv_load" OWNER TO "loader";
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
