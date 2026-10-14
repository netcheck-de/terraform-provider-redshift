-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"loaders"}

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
GRANT ALTER ON TEMPLATE "admin"."serving"."csv_load" TO ROLE "loaders";
-- params: {}

-- database: admin
GRANT USAGE ON TEMPLATE "admin"."serving"."csv_load" TO ROLE "loaders";
-- params: {}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"loaders"}

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
