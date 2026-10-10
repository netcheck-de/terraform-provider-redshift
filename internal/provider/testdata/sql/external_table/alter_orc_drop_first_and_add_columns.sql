ALTER TABLE "example_external"."events" ADD COLUMN "note" varchar(16);

ALTER TABLE "example_external"."events" DROP COLUMN "id";
