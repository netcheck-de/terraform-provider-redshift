-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: analytics
SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share;
-- params: {"share":"producer"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"analytics","schema":"serving"}

-- database: analytics
SHOW GRANTS ON SCHEMA "serving";
-- params: {}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: analytics
SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share;
-- params: {"share":"producer"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"analytics","schema":"serving"}

-- database: analytics
SHOW GRANTS ON SCHEMA "serving";
-- params: {}

-- database: analytics
REVOKE SELECT FOR TABLES IN SCHEMA "serving" FROM DATASHARE "producer";
-- params: {}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: analytics
SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share;
-- params: {"share":"producer"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"analytics","schema":"serving"}

-- database: analytics
SHOW GRANTS ON SCHEMA "serving";
-- params: {}
