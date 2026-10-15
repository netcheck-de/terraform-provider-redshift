-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :role;
-- params: {"role":"example:readers"}

-- database: analytics
SHOW GRANTS FOR ROLE "example:readers" FROM DATABASE "analytics";
-- params: {}

-- database: analytics
GRANT CREATE ON DATABASE "analytics" TO ROLE "example:readers";
-- params: {}

-- database: admin
SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;
-- params: {"database":"analytics"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :role;
-- params: {"role":"example:readers"}

-- database: analytics
SHOW GRANTS FOR ROLE "example:readers" FROM DATABASE "analytics";
-- params: {}
