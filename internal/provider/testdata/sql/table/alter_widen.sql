ALTER TABLE "serving"."events" ALTER COLUMN "note" TYPE character varying(65535);

ALTER TABLE "serving"."events" ALTER COLUMN "note" ENCODE ZSTD;
