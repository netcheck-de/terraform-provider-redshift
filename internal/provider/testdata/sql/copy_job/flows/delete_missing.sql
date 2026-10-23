-- database: admin
SELECT * FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}
