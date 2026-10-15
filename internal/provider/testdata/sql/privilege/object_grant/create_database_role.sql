-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SHOW GRANTS ON DATABASE "warehouse";
-- params: {}

-- database: warehouse
GRANT TEMPORARY ON DATABASE "warehouse" TO ROLE "readers";
-- params: {}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SHOW GRANTS ON DATABASE "warehouse";
-- params: {}
