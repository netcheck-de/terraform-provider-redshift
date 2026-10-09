-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT object_name FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :object AND object_type IN ('table', 'view', 'late binding view', 'materialized view');
-- params: {"object":"serving.table","share":"producer"}
