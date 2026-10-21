ALTER TABLE "serving"."events" RENAME COLUMN "id" TO "event_id";

ALTER TABLE "serving"."events" DROP CONSTRAINT "events_id_fkey";

ALTER TABLE "serving"."events" DROP CONSTRAINT "events_id_key";
