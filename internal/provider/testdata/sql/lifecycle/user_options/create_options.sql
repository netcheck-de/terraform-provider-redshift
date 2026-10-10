-- database: admin
CREATE USER "grafana" PASSWORD 'InitialPass123' NOCREATEUSER NOCREATEDB SYSLOG ACCESS UNRESTRICTED VALID UNTIL '2030-06-30 21:59:00+00' CONNECTION LIMIT 10 SESSION TIMEOUT 300;
-- params: {}

-- database: admin
ALTER USER "grafana" SET search_path TO '$user', 'Odd"Schema', 'it''s \\path';
-- params: {}

-- database: admin
ALTER USER "grafana" SET query_group TO 'it''s \\x';
-- params: {}

-- database: admin
ALTER USER "grafana" SET timezone TO 'Europe/Berlin';
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb, valuntil, array_to_string(useconfig, chr(30)) AS useconfig FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}

-- database: admin
SELECT connection_limit, syslog_access, session_timeout, external_user_id FROM svv_user_info WHERE user_name = :name;
-- params: {"name":"grafana"}
