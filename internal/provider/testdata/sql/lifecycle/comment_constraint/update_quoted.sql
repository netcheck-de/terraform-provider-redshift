-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_constraint k JOIN pg_class o ON o.oid = k.conrelid JOIN pg_namespace n ON n.oid = o.relnamespace LEFT JOIN pg_description d ON d.objoid = k.oid AND d.classoid = 'pg_constraint'::regclass AND d.objsubid = 0 WHERE k.conname = :constraint AND o.relname = :name AND n.nspname = :schema;
-- params: {"constraint":"Odd\"Key","name":"Odd\"Table","schema":"Odd\"Schema"}

-- database: analytics
COMMENT ON CONSTRAINT "Odd""Key" ON "Odd""Schema"."Odd""Table" IS 'Owner''s \\notes';
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: analytics
SELECT COALESCE(d.description, '') AS text FROM pg_constraint k JOIN pg_class o ON o.oid = k.conrelid JOIN pg_namespace n ON n.oid = o.relnamespace LEFT JOIN pg_description d ON d.objoid = k.oid AND d.classoid = 'pg_constraint'::regclass AND d.objsubid = 0 WHERE k.conname = :constraint AND o.relname = :name AND n.nspname = :schema;
-- params: {"constraint":"Odd\"Key","name":"Odd\"Table","schema":"Odd\"Schema"}
