-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_summary","schema":"serving"}

-- database: admin
SELECT autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database AND RTRIM(schema_name) = :schema AND RTRIM(name) = :name;
-- params: {"database":"admin","name":"sales_summary","schema":"serving"}

-- database: admin
ALTER MATERIALIZED VIEW "serving"."sales_summary" ALTER DISTSTYLE KEY DISTKEY "label";
-- params: {}

-- database: admin
ALTER MATERIALIZED VIEW "serving"."sales_summary" ALTER COMPOUND SORTKEY ("label", "sales");
-- params: {}

-- database: admin
ALTER TABLE "serving"."sales_summary" OWNER TO "reporter";
-- params: {}

-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_summary","schema":"serving"}

-- database: admin
SELECT autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database AND RTRIM(schema_name) = :schema AND RTRIM(name) = :name;
-- params: {"database":"admin","name":"sales_summary","schema":"serving"}
