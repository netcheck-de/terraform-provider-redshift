-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname !~ '^pg_' AND schemaname <> 'information_schema' AND schemaname = :schema ORDER BY schemaname, viewname;
-- params: {"schema":"other"}
