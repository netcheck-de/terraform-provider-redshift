-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"template:loader"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"admin"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"admin","schema":"serving"}

-- database: admin
SHOW TEMPLATES FROM SCHEMA "admin"."serving" LIKE 'csv_load';
-- params: {}

-- database: admin
SHOW GRANTS ON TEMPLATE "admin"."serving"."csv_load";
-- params: {}

-- database: admin
GRANT USAGE ON TEMPLATE "admin"."serving"."csv_load" TO "template:loader" WITH GRANT OPTION;
-- params: {}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"template:loader"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"admin"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"admin","schema":"serving"}

-- database: admin
SHOW TEMPLATES FROM SCHEMA "admin"."serving" LIKE 'csv_load';
-- params: {}

-- database: admin
SHOW GRANTS ON TEMPLATE "admin"."serving"."csv_load";
-- params: {}
