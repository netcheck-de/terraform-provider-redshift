ALTER TABLE "serving"."events" ALTER COMPOUND SORTKEY ("note");

ALTER TABLE "serving"."events" ALTER DISTSTYLE EVEN;
