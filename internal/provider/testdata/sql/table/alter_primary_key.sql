ALTER TABLE "serving"."events" DROP CONSTRAINT "events_pkey";

ALTER TABLE "serving"."events" ADD COLUMN "code" character(2) DEFAULT 'xx' NOT NULL;

ALTER TABLE "serving"."events" DROP COLUMN "note";

ALTER TABLE "serving"."events" ADD PRIMARY KEY ("id", "code");
