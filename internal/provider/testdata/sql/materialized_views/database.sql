-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname !~ '^pg_' AND schemaname <> 'information_schema' ORDER BY schemaname, viewname;
-- params: {}

-- database: admin
SELECT RTRIM(schema_name) AS schema_name, RTRIM(name) AS name, autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database;
-- params: {"database":"admin"}
