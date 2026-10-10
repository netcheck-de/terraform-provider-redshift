CREATE VIEW "serving"."sales_view" AS SELECT id FROM serving.sales -- latest
 WITH NO SCHEMA BINDING;

ALTER TABLE "serving"."sales_view" OWNER TO "analyst";
