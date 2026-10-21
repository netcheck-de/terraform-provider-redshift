-- database: admin
COPY JOB ALTER events_load AUTO ON;
-- params: {}

-- database: admin
SELECT * FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}
