-- database: admin
CREATE MATERIALIZED VIEW "serving"."sales_summary" AUTO REFRESH YES AS SELECT label, COUNT(*) AS sales FROM serving.sales GROUP BY label;
-- params: {}

-- database: admin
ALTER TABLE "serving"."sales_summary" OWNER TO "analyst";
-- params: {}

-- database: admin
SELECT schemaname, viewname, viewowner, definition FROM pg_views WHERE schemaname = :schema AND viewname = :name;
-- params: {"name":"sales_summary","schema":"serving"}

-- database: admin
SELECT autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database AND RTRIM(schema_name) = :schema AND RTRIM(name) = :name;
-- params: {"database":"admin","name":"sales_summary","schema":"serving"}
