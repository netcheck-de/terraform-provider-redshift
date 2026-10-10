ALTER TABLE "example_external"."events" PARTITION ("event_date" = '2024-01-01') SET LOCATION 's3://example-bucket/events/2024/01/01/';
