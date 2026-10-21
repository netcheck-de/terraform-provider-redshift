-- database: admin
SELECT usesuper FROM pg_user WHERE usename = current_user;
-- params: {}

-- database: admin
SELECT object_name, include_new FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :schema AND object_type IN ('schema', 'schemas');
-- params: {"schema":"serving","share":"producer"}

-- database: admin
SELECT table_name AS member_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_type <> 'EXTERNAL TABLE';
-- params: {"database":"admin","schema":"serving"}

-- database: admin
SELECT object_name FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_type IN ('table', 'view', 'late binding view', 'materialized view');
-- params: {"share":"producer"}
