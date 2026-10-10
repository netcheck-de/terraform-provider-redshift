ALTER TABLE "serving"."events" DROP CONSTRAINT "events_id_fkey";

ALTER TABLE "serving"."events" DROP CONSTRAINT "odd""key";

ALTER TABLE "serving"."events" DROP CONSTRAINT "events_label_key";

ALTER TABLE "serving"."events" ADD COLUMN "extra" DATE ENCODE AZ64;

ALTER TABLE "serving"."events" ALTER COMPOUND SORTKEY ("extra");

ALTER TABLE "serving"."events" ALTER DISTKEY "extra";

ALTER TABLE "serving"."events" ALTER COLUMN "id" ENCODE ZSTD;

ALTER TABLE "serving"."events" DROP COLUMN "note";

ALTER TABLE "serving"."events" ADD UNIQUE ("label", "extra");

ALTER TABLE "serving"."events" OWNER TO "analyst";
