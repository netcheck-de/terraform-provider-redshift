-- database: admin
SELECT usesuper FROM pg_user WHERE usename = current_user;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT object_name, include_new FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :schema AND object_type IN ('schema', 'schemas');
-- params: {"schema":"serving","share":"producer"}

-- database: warehouse
SELECT table_name AS member_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_type <> 'EXTERNAL TABLE';
-- params: {"database":"warehouse","schema":"serving"}

-- database: warehouse
SELECT object_name FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_type IN ('table', 'view', 'late binding view', 'materialized view');
-- params: {"share":"producer"}

-- database: warehouse
ALTER DATASHARE "producer" ADD ALL TABLES IN SCHEMA "serving";
-- params: {}

-- database: admin
SELECT usesuper FROM pg_user WHERE usename = current_user;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT object_name, include_new FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :schema AND object_type IN ('schema', 'schemas');
-- params: {"schema":"serving","share":"producer"}

-- database: warehouse
SELECT table_name AS member_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_type <> 'EXTERNAL TABLE';
-- params: {"database":"warehouse","schema":"serving"}

-- database: warehouse
SELECT object_name FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_type IN ('table', 'view', 'late binding view', 'materialized view');
-- params: {"share":"producer"}
