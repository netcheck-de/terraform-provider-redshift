-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_summary","schema":"serving"}

-- database: warehouse
SELECT autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database AND RTRIM(schema_name) = :schema AND RTRIM(name) = :name;
-- params: {"database":"warehouse","name":"sales_summary","schema":"serving"}
