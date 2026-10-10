CREATE TABLE "serving"."staging" ("payload" super) BACKUP YES DISTSTYLE EVEN SORTKEY AUTO;

ALTER TABLE "serving"."staging" OWNER TO "etl";
