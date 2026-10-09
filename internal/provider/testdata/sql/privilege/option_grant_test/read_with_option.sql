-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"analyst"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"warehouse","name":"orders","schema":"serving"}

-- database: warehouse
SHOW GRANTS ON TABLE "warehouse"."serving"."orders";
-- params: {}
