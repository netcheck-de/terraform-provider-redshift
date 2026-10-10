ALTER TABLE "serving"."events" ALTER SORTKEY NONE;

ALTER TABLE "serving"."events" DROP COLUMN "note";

ALTER TABLE "serving"."events" ALTER SORTKEY AUTO;
