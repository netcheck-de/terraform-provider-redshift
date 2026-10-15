-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"warehouse","name":"events","schema":"serving"}

-- database: warehouse
SELECT column_name, privilege_type FROM svv_column_privileges WHERE namespace_name = :schema AND relation_name = :object AND identity_name = :grantee AND identity_type = :identity_type ORDER BY privilege_type, column_name;
-- params: {"grantee":"example:readers","identity_type":"role","object":"events","schema":"serving"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"warehouse","name":"events","schema":"serving"}

-- database: warehouse
SELECT column_name, privilege_type FROM svv_column_privileges WHERE namespace_name = :schema AND relation_name = :object AND identity_name = :grantee AND identity_type = :identity_type ORDER BY privilege_type, column_name;
-- params: {"grantee":"example:readers","identity_type":"role","object":"events","schema":"serving"}
