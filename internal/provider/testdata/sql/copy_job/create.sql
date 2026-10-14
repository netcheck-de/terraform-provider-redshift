COPY "serving"."events" ("id", "label") FROM 's3://example-bucket/events/' IAM_ROLE 'arn:aws:iam::123456789012:role/loader' FORMAT AS CSV IGNOREHEADER 1 JOB CREATE events_load AUTO OFF;
