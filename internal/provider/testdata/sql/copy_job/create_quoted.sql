COPY "Odd""Schema"."Odd""Table" ("Odd""Column") FROM 's3://example-bucket/it''s/' IAM_ROLE 'arn:aws:iam::123456789012:role/loader' FORMAT AS CSV NULL AS '\\N' JOB CREATE events_load AUTO OFF;
