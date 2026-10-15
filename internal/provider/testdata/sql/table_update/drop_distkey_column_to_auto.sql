-- database: admin
SELECT c.relname AS table_name, u.usename AS owner, c.reldiststyle AS diststyle, ci.releffectivediststyle AS effective_diststyle FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_user u ON u.usesysid = c.relowner LEFT JOIN pg_class_info ci ON ci.reloid = c.oid WHERE n.nspname = :schema AND c.relname = :name AND c.relkind = 'r';
-- params: {"name":"events","schema":"serving"}

-- database: admin
SELECT a.attnum AS position, a.attname AS column_name, format_type(a.atttypid, a.atttypmod) AS data_type, a.attnotnull AS not_null FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = :schema AND c.relname = :name AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum;
-- params: {"name":"events","schema":"serving"}

-- database: admin
SELECT column_name, column_default, encoding, distkey, sortkey FROM svv_redshift_columns WHERE database_name = :database AND schema_name = :schema AND table_name = :name ORDER BY ordinal_position;
-- params: {"database":"admin","name":"events","schema":"serving"}

-- database: admin
SELECT con.conname AS constraint_name, con.contype AS constraint_type, pg_get_constraintdef(con.oid) AS definition, rn.nspname AS referenced_schema, rc.relname AS referenced_table FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_class rc ON rc.oid = con.confrelid LEFT JOIN pg_namespace rn ON rn.oid = rc.relnamespace WHERE n.nspname = :schema AND c.relname = :name AND con.contype IN ('p', 'u', 'f') ORDER BY con.conname;
-- params: {"name":"events","schema":"serving"}

-- database: admin
SELECT sortkey1 FROM svv_table_info WHERE "schema" = :schema AND "table" = :name;
-- params: {"name":"events","schema":"serving"}

-- database: admin
ALTER TABLE "serving"."events" ALTER DISTSTYLE EVEN;
-- params: {}

-- database: admin
ALTER TABLE "serving"."events" DROP COLUMN "note";
-- params: {}

-- database: admin
ALTER TABLE "serving"."events" ALTER DISTSTYLE AUTO;
-- params: {}

-- database: admin
SELECT c.relname AS table_name, u.usename AS owner, c.reldiststyle AS diststyle, ci.releffectivediststyle AS effective_diststyle FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_user u ON u.usesysid = c.relowner LEFT JOIN pg_class_info ci ON ci.reloid = c.oid WHERE n.nspname = :schema AND c.relname = :name AND c.relkind = 'r';
-- params: {"name":"events","schema":"serving"}

-- database: admin
SELECT a.attnum AS position, a.attname AS column_name, format_type(a.atttypid, a.atttypmod) AS data_type, a.attnotnull AS not_null FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = :schema AND c.relname = :name AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum;
-- params: {"name":"events","schema":"serving"}

-- database: admin
SELECT column_name, column_default, encoding, distkey, sortkey FROM svv_redshift_columns WHERE database_name = :database AND schema_name = :schema AND table_name = :name ORDER BY ordinal_position;
-- params: {"database":"admin","name":"events","schema":"serving"}

-- database: admin
SELECT con.conname AS constraint_name, con.contype AS constraint_type, pg_get_constraintdef(con.oid) AS definition, rn.nspname AS referenced_schema, rc.relname AS referenced_table FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_class rc ON rc.oid = con.confrelid LEFT JOIN pg_namespace rn ON rn.oid = rc.relnamespace WHERE n.nspname = :schema AND c.relname = :name AND con.contype IN ('p', 'u', 'f') ORDER BY con.conname;
-- params: {"name":"events","schema":"serving"}

-- database: admin
SELECT sortkey1 FROM svv_table_info WHERE "schema" = :schema AND "table" = :name;
-- params: {"name":"events","schema":"serving"}
