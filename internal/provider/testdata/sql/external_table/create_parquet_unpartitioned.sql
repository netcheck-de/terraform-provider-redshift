CREATE EXTERNAL TABLE "example_external"."events" ("id" INTEGER, "label" VARCHAR(64)) STORED AS PARQUET LOCATION 's3://example-bucket/events/';
