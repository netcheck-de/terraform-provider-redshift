ALTER TABLE "serving"."events" ALTER COMPOUND SORTKEY ("id");

ALTER TABLE "serving"."events" ALTER DISTKEY "id";

ALTER TABLE "serving"."events" DROP COLUMN "note";
