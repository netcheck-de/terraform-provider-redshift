-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_class o JOIN pg_namespace n ON n.oid = o.relnamespace LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0 WHERE o.relname = :name AND n.nspname = :schema AND o.relkind IN ('r', 'm');
-- params: {"name":"table","schema":"serving"}

-- database: analytics
COMMENT ON TABLE "serving"."table" IS NULL;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_class o JOIN pg_namespace n ON n.oid = o.relnamespace LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0 WHERE o.relname = :name AND n.nspname = :schema AND o.relkind IN ('r', 'm');
-- params: {"name":"table","schema":"serving"}
