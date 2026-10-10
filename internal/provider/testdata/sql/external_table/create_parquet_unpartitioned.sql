CREATE EXTERNAL TABLE "example_external"."events" ("id" integer, "label" varchar(64)) STORED AS PARQUET LOCATION 's3://example-bucket/events/';
