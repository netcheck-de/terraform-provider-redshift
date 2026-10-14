-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT job_name, iam_role, is_auto FROM sys_copy_job WHERE job_name = :name;
-- params: {"name":"events_load"}
