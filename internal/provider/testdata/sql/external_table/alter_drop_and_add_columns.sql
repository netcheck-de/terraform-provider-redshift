ALTER TABLE "example_external"."events" ADD COLUMN "note" VARCHAR(256);

ALTER TABLE "example_external"."events" DROP COLUMN "id";
