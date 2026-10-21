-- database: admin
SELECT * FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}

-- database: admin
COPY JOB DROP events_load;
-- params: {}

-- database: admin
SELECT * FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}
