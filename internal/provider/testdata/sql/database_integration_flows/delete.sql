-- database: admin
SHOW DATABASES LIKE 'replica';
-- params: {}

-- database: admin
SELECT u.usename AS owner, d.datconnlimit AS connection_limit FROM pg_database_info d LEFT JOIN pg_user u ON u.usesysid = d.datdba WHERE d.datname = :name;
-- params: {"name":"replica"}

-- database: admin
SELECT TRIM(integration_id) AS integration_id, TRIM(source_database) AS source_database, refresh_interval, is_history_mode, auto_remediation FROM svv_integration WHERE TRIM(target_database) = :name;
-- params: {"name":"replica"}

-- database: admin
DROP DATABASE "replica";
-- params: {}
