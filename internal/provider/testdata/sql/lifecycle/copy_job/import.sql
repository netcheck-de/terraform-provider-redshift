-- database: admin
SELECT job_name, iam_role, is_auto FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}
