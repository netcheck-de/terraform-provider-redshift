-- database: admin
CREATE VIEW "serving"."sales_view" AS SELECT id, label FROM serving.sales;
-- params: {}

-- database: admin
ALTER TABLE "serving"."sales_view" OWNER TO "analyst";
-- params: {}

-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_view","schema":"serving"}
