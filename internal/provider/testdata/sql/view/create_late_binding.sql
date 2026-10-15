CREATE VIEW "serving"."sales_view" AS SELECT id, label FROM serving.sales WITH NO SCHEMA BINDING;

ALTER TABLE "serving"."sales_view" OWNER TO "analyst";
