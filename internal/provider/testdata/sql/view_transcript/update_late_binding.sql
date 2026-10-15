-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_view","schema":"serving"}

-- database: admin
CREATE OR REPLACE VIEW "serving"."sales_view" AS SELECT id, label FROM serving.sales WITH NO SCHEMA BINDING;
-- params: {}

-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_view","schema":"serving"}
