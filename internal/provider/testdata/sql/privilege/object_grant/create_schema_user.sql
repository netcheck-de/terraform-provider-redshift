-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: warehouse
SHOW GRANTS ON SCHEMA "warehouse"."serving";
-- params: {}

-- database: warehouse
GRANT USAGE ON SCHEMA "warehouse"."serving" TO "loader";
-- params: {}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: warehouse
SHOW GRANTS ON SCHEMA "warehouse"."serving";
-- params: {}
