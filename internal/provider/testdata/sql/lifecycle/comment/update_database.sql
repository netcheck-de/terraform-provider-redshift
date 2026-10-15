-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_database o LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_database'::regclass AND d.objsubid = 0 WHERE o.datname = :name;
-- params: {"name":"analytics"}

-- database: analytics
COMMENT ON DATABASE "analytics" IS 'Owner''s \\notes';
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_database o LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_database'::regclass AND d.objsubid = 0 WHERE o.datname = :name;
-- params: {"name":"analytics"}
