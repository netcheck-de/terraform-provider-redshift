SELECT TRIM(auth_method) AS auth_method FROM sys_connection_log WHERE session_id = pg_backend_pid() AND user_name = current_user AND event = 'authenticated' ORDER BY record_time DESC LIMIT 1;
