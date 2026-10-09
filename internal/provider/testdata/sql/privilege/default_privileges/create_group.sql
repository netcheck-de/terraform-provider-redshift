-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND schema_name = :schema AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);
-- params: {"grantee":"readers","kind":"GROUP","object_type":"RELATION","owner":"loader","schema":"serving"}

-- database: warehouse
ALTER DEFAULT PRIVILEGES FOR USER "loader" IN SCHEMA "serving" GRANT SELECT ON TABLES TO GROUP "readers";
-- params: {}

-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"loader"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND schema_name = :schema AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);
-- params: {"grantee":"readers","kind":"GROUP","object_type":"RELATION","owner":"loader","schema":"serving"}
