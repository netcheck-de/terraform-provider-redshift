ALTER TABLE "example_external"."events" ADD PARTITION ("salesmonth" = '2008-01', "Event" = '101') LOCATION 's3://example-bucket/events/event_date=2024-01-01/';
