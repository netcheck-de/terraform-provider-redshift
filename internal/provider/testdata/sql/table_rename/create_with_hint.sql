CREATE TABLE "serving"."events" ("event_id" BIGINT IDENTITY(1, 1) ENCODE AZ64 NOT NULL, "label" CHARACTER VARYING(64) DEFAULT 'none' ENCODE LZO, PRIMARY KEY ("event_id")) BACKUP YES DISTSTYLE KEY DISTKEY ("event_id") COMPOUND SORTKEY ("event_id");

ALTER TABLE "serving"."events" OWNER TO "admin";
