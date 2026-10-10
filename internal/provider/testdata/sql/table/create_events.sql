CREATE TABLE "serving"."events" ("id" bigint IDENTITY(1, 1) ENCODE AZ64 NOT NULL, "label" character varying(64) DEFAULT 'none' ENCODE LZO, PRIMARY KEY ("id")) BACKUP YES DISTSTYLE KEY DISTKEY ("id") COMPOUND SORTKEY ("id");

ALTER TABLE "serving"."events" OWNER TO "admin";
