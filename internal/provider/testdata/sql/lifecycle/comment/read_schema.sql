-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_namespace o LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_namespace'::regclass AND d.objsubid = 0 WHERE o.nspname = :name;
-- params: {"name":"serving"}
