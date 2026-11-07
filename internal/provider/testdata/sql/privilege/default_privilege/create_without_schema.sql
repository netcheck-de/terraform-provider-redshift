-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND (schema_name IS NULL OR schema_name = '') AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);
-- params: {"grantee":"readers","kind":"ROLE","object_type":"RELATION","owner":"loader"}

-- database: warehouse
ALTER DEFAULT PRIVILEGES FOR USER "loader" GRANT SELECT ON TABLES TO ROLE "readers";
-- params: {}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND (schema_name IS NULL OR schema_name = '') AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);
-- params: {"grantee":"readers","kind":"ROLE","object_type":"RELATION","owner":"loader"}
