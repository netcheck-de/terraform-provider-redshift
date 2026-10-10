-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;
-- params: {"grantee":"example:readers","identity_type":"role","language":"plpgsql"}

-- database: warehouse
GRANT USAGE ON LANGUAGE PLPGSQL TO ROLE "example:readers";
-- params: {}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;
-- params: {"grantee":"example:readers","identity_type":"role","language":"plpgsql"}
