-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT n.nspname AS schema_name, u.usename AS owner FROM pg_namespace n JOIN pg_user u ON n.nspowner = u.usesysid LEFT JOIN pg_external_schema e ON e.esoid = n.oid WHERE e.esoid IS NULL ORDER BY n.nspname;
-- params: {}

-- database: analytics
SELECT TRIM(schema_name) AS schema_name, quota FROM svv_redshift_schema_quota WHERE TRIM(database_name) = :database;
-- params: {"database":"analytics"}

-- database: analytics
SELECT usename AS name, usesuper AS superuser FROM pg_user WHERE usename = current_user;
-- params: {}
