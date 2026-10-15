CREATE VIEW "serving"."sales_view" AS SELECT id
  FROM serving.sales;

ALTER TABLE "serving"."sales_view" OWNER TO "analyst";
