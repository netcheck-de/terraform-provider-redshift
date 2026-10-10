ALTER TABLE "serving"."events" ALTER SORTKEY NONE;

ALTER TABLE "serving"."events" ALTER DISTSTYLE EVEN;

ALTER TABLE "serving"."events" DROP COLUMN "note";

ALTER TABLE "serving"."events" ALTER SORTKEY AUTO;

ALTER TABLE "serving"."events" ALTER DISTSTYLE AUTO;
