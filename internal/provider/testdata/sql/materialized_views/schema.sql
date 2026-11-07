-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname !~ '^pg_' AND schemaname <> 'information_schema' AND schemaname = :schema ORDER BY schemaname, viewname;
-- params: {"schema":"staging"}

-- database: admin
SELECT RTRIM(schema_name) AS schema_name, RTRIM(name) AS name, autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database AND RTRIM(schema_name) = :schema;
-- params: {"database":"admin","schema":"staging"}
