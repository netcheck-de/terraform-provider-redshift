ALTER TABLE "serving"."staging" RENAME COLUMN "old""name" TO "new""name";

ALTER TABLE "serving"."staging" ALTER COLUMN "new""name" TYPE CHARACTER VARYING(32);

ALTER TABLE "serving"."staging" ALTER COLUMN "new""name" ENCODE ZSTD;
