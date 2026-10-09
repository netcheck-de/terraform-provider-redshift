-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_class o JOIN pg_namespace n ON n.oid = o.relnamespace JOIN pg_attribute a ON a.attrelid = o.oid AND a.attname = :column AND a.attnum > 0 AND NOT a.attisdropped LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = a.attnum WHERE o.relname = :name AND n.nspname = :schema;
-- params: {"column":"column","name":"table","schema":"serving"}

-- database: analytics
COMMENT ON COLUMN "serving"."table"."column" IS NULL;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_class o JOIN pg_namespace n ON n.oid = o.relnamespace JOIN pg_attribute a ON a.attrelid = o.oid AND a.attname = :column AND a.attnum > 0 AND NOT a.attisdropped LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = a.attnum WHERE o.relname = :name AND n.nspname = :schema;
-- params: {"column":"column","name":"table","schema":"serving"}
