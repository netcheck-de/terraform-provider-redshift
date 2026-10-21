-- database: admin
SELECT usesuper FROM pg_user WHERE usename = current_user;
-- params: {}

-- database: admin
SELECT object_name, include_new FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :schema AND object_type IN ('schema', 'schemas');
-- params: {"schema":"serving","share":"producer"}

-- database: admin
SELECT p.proname AS member_name FROM pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_language l ON l.oid = p.prolang WHERE n.nspname = :schema AND p.prokind = 'f' AND l.lanname = 'sql';
-- params: {"schema":"serving"}

-- database: admin
SELECT object_name FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_type = 'function';
-- params: {"share":"producer"}
