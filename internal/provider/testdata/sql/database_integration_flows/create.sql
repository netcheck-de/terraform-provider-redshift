-- database: admin
CREATE DATABASE "replica" FROM INTEGRATION 'a1b2c3d4-5678-90ab-cdef-example11111' DATABASE "it's""orders" SET ACCEPTINVCHARS = FALSE QUERY_ALL_STATES = TRUE REFRESH_INTERVAL 600 TRUNCATECOLUMNS = TRUE HISTORY_MODE = TRUE AUTO_REMEDIATION = TRUE OWNER "Etl""Owner" CONNECTION LIMIT 25 ISOLATION LEVEL SERIALIZABLE;
-- params: {}

-- database: admin
SHOW DATABASES LIKE 'replica';
-- params: {}

-- database: admin
SELECT u.usename AS owner, d.datconnlimit AS connection_limit FROM pg_database_info d LEFT JOIN pg_user u ON u.usesysid = d.datdba WHERE d.datname = :name;
-- params: {"name":"replica"}

-- database: admin
SELECT TRIM(integration_id) AS integration_id, TRIM(source_database) AS source_database, refresh_interval, is_history_mode, auto_remediation FROM svv_integration WHERE TRIM(target_database) = :name;
-- params: {"name":"replica"}
