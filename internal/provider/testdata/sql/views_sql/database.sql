SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname !~ '^pg_' AND schemaname <> 'information_schema' ORDER BY schemaname, viewname;
