ALTER TABLE "Lake""Schema"."Odd""Table" ADD COLUMN "Other""Col" DATE;

ALTER TABLE "Lake""Schema"."Odd""Table" SET LOCATION 's3://bucket/it''s \\moved/';

ALTER TABLE "Lake""Schema"."Odd""Table" SET TABLE PROPERTIES ('numRows' = '1''0\\0');
