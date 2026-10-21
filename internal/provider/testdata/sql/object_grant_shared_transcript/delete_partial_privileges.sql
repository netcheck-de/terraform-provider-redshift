-- database: admin
SELECT usesuper FROM pg_user WHERE usename = current_user;
-- params: {}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"reader"}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';
-- params: {"database":"shared_db"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"shared_db","schema":"serving"}

-- database: admin
SELECT DISTINCT function_name AS object_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema ORDER BY object_name;
-- params: {"database":"shared_db","schema":"serving"}

-- database: admin
SHOW GRANTS ON FUNCTION "shared_db"."serving"."first";
-- params: {}

-- database: admin
SHOW GRANTS ON FUNCTION "shared_db"."serving"."second";
-- params: {}

-- database: admin
SELECT usesuper FROM pg_user WHERE usename = current_user;
-- params: {}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"reader"}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';
-- params: {"database":"shared_db"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"shared_db","schema":"serving"}

-- database: admin
SELECT DISTINCT function_name AS object_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema ORDER BY object_name;
-- params: {"database":"shared_db","schema":"serving"}

-- database: admin
SHOW GRANTS ON FUNCTION "shared_db"."serving"."first";
-- params: {}

-- database: admin
SHOW GRANTS ON FUNCTION "shared_db"."serving"."second";
-- params: {}

-- database: admin
REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA "shared_db"."serving" FROM "reader";
-- params: {}

-- database: admin
SELECT usesuper FROM pg_user WHERE usename = current_user;
-- params: {}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"reader"}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';
-- params: {"database":"shared_db"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"shared_db","schema":"serving"}

-- database: admin
SELECT DISTINCT function_name AS object_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema ORDER BY object_name;
-- params: {"database":"shared_db","schema":"serving"}

-- database: admin
SHOW GRANTS ON FUNCTION "shared_db"."serving"."first";
-- params: {}

-- database: admin
SHOW GRANTS ON FUNCTION "shared_db"."serving"."second";
-- params: {}
