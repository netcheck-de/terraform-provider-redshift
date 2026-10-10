CREATE MATERIALIZED VIEW "serving"."sales_summary" AUTO REFRESH NO AS SELECT label, COUNT(*) AS sales FROM serving.sales GROUP BY label;
