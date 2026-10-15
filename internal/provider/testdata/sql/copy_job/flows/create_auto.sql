-- database: admin
COPY "serving"."events" ("id", "label") FROM 's3://example-bucket/events/' IAM_ROLE 'arn:aws:iam::123456789012:role/loader' FORMAT AS CSV IGNOREHEADER 1 JOB CREATE events_load AUTO ON;
-- params: {}

-- database: admin
SELECT job_name, iam_role, is_auto FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}
