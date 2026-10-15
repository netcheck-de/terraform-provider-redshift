-- database: admin
COPY JOB ALTER events_load AUTO ON;
-- params: {}

-- database: admin
SELECT job_name, iam_role, is_auto FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}
