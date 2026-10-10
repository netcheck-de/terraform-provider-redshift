-- database: admin
SELECT n.nspname AS schema_name, u.usename AS owner FROM pg_namespace n JOIN pg_user u ON n.nspowner = u.usesysid WHERE n.nspname = :name;
-- params: {"name":"serving"}

-- database: admin
SELECT quota FROM svv_redshift_schema_quota WHERE TRIM(database_name) = :database AND TRIM(schema_name) = :name;
-- params: {"database":"admin","name":"serving"}
