CREATE TABLE "serving"."staging" ("payload" SUPER) DISTSTYLE AUTO;

ALTER TABLE "serving"."staging" ALTER SORTKEY NONE;

ALTER TABLE "serving"."staging" OWNER TO "etl";
