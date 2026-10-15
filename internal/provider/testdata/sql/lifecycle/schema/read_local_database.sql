-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT n.nspname AS schema_name, u.usename AS owner FROM pg_namespace n JOIN pg_user u ON n.nspowner = u.usesysid WHERE n.nspname = :name;
-- params: {"name":"serving"}

-- database: warehouse
SELECT quota FROM svv_redshift_schema_quota WHERE TRIM(database_name) = :database AND TRIM(schema_name) = :name;
-- params: {"database":"warehouse","name":"serving"}

-- database: warehouse
SELECT usename AS name, usesuper AS superuser FROM pg_user WHERE usename = current_user;
-- params: {}
