-- database: warehouse
CREATE VIEW "serving"."sales_view" AS SELECT id, label FROM serving.sales;
-- params: {}

-- database: warehouse
ALTER TABLE "serving"."sales_view" OWNER TO "analyst";
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_view","schema":"serving"}
