-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;
-- params: {"grantee":"readers","identity_type":"group","language":"plpgsql"}

-- database: warehouse
GRANT USAGE ON LANGUAGE plpgsql TO GROUP "readers";
-- params: {}

-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;
-- params: {"grantee":"readers","identity_type":"group","language":"plpgsql"}
