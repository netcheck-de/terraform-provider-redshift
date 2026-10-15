-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND (schema_name IS NULL OR schema_name = '') AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);
-- params: {"grantee":"public","kind":"PUBLIC","object_type":"PROCEDURE","owner":"loader"}

-- database: warehouse
ALTER DEFAULT PRIVILEGES FOR USER "loader" GRANT EXECUTE ON PROCEDURES TO PUBLIC;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND (schema_name IS NULL OR schema_name = '') AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);
-- params: {"grantee":"public","kind":"PUBLIC","object_type":"PROCEDURE","owner":"loader"}
