-- database: admin
ALTER USER "grafana" PASSWORD 'Back''Pass\\1';
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb, valuntil, array_to_string(useconfig, chr(30)) AS useconfig FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}

-- database: admin
SELECT connection_limit, syslog_access, session_timeout, external_user_id FROM svv_user_info WHERE user_name = :name;
-- params: {"name":"grafana"}
