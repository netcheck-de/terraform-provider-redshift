ALTER TABLE "serving"."events" ALTER DISTSTYLE EVEN;

ALTER TABLE "serving"."events" DROP COLUMN "note";

ALTER TABLE "serving"."events" ALTER DISTSTYLE AUTO;
