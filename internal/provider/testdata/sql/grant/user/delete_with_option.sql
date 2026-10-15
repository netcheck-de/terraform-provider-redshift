-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"scoped:analyst"}

-- database: admin
SHOW GRANTS ON DATABASE "analytics" FOR "scoped:analyst";
-- params: {}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"scoped:analyst"}

-- database: admin
SHOW GRANTS ON DATABASE "analytics" FOR "scoped:analyst";
-- params: {}

-- database: admin
REVOKE INSERT FOR TABLES IN DATABASE "analytics" FROM "scoped:analyst";
-- params: {}

-- database: admin
REVOKE SELECT FOR TABLES IN DATABASE "analytics" FROM "scoped:analyst";
-- params: {}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"scoped:analyst"}

-- database: admin
SHOW GRANTS ON DATABASE "analytics" FOR "scoped:analyst";
-- params: {}
