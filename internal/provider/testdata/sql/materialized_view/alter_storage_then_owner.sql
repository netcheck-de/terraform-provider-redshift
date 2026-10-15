ALTER MATERIALIZED VIEW "serving"."sales_summary" ALTER DISTSTYLE EVEN;

ALTER MATERIALIZED VIEW "serving"."sales_summary" ALTER COMPOUND SORTKEY ("label");

ALTER MATERIALIZED VIEW "serving"."sales_summary" AUTO REFRESH NO;

ALTER TABLE "serving"."sales_summary" OWNER TO "reporter";
