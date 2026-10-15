-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :role;
-- params: {"role":"example:readers"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"analytics","schema":"serving"}

-- database: analytics
SHOW GRANTS FOR ROLE "example:readers" FROM DATABASE "analytics";
-- params: {}

-- database: analytics
GRANT SELECT FOR TABLES IN SCHEMA "serving" DATABASE "analytics" TO ROLE "example:readers";
-- params: {}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :role;
-- params: {"role":"example:readers"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"analytics","schema":"serving"}

-- database: analytics
SHOW GRANTS FOR ROLE "example:readers" FROM DATABASE "analytics";
-- params: {}
