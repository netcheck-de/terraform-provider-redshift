-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_view","schema":"serving"}

-- database: admin
ALTER TABLE "serving"."sales_view" OWNER TO "reporter";
-- params: {}

-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_view","schema":"serving"}
