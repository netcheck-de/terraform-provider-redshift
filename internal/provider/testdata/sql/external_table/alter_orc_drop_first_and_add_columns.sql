ALTER TABLE "example_external"."events" ADD COLUMN "note" VARCHAR(16);

ALTER TABLE "example_external"."events" DROP COLUMN "id";
