SELECT TRIM(integration_id) AS integration_id, TRIM(source_database) AS source_database, refresh_interval, is_history_mode, auto_remediation FROM svv_integration WHERE TRIM(target_database) = :name;
