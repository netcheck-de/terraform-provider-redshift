-- database: admin
SELECT TRIM(auth_method) AS auth_method FROM sys_connection_log WHERE session_id = pg_backend_pid() AND user_name = current_user AND event = 'authenticated' ORDER BY record_time DESC LIMIT 1;
-- params: {}

-- database: admin
SHOW DATABASES LIKE 'sales_db@finance-catalog';
-- params: {}

-- database: admin
SHOW GRANTS FOR ROLE "myidc:audit_team" FROM DATABASE "sales_db@finance-catalog";
-- params: {}
