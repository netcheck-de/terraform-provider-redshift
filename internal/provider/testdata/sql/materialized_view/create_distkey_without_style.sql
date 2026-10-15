CREATE MATERIALIZED VIEW "serving"."sales_summary" DISTKEY ("label") AUTO REFRESH YES AS SELECT label, COUNT(*) AS sales FROM serving.sales GROUP BY label;

ALTER TABLE "serving"."sales_summary" OWNER TO "analyst";
