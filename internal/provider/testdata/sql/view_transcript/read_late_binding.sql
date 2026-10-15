-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_view","schema":"serving"}
