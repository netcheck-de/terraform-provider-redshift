ALTER TABLE "example_external"."events" ADD PARTITION ("event_date" = '2024-01-01') LOCATION 's3://example-bucket/events/event_date=2024-01-01/';
